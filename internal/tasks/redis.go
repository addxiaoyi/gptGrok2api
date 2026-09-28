package tasks

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RedisQueue is a dependency-free Redis task backend. It uses only the small
// RESP subset needed by the task lifecycle, so deployments without the Go
// module cache can still build the service.
type RedisQueue struct {
	addr     string
	password string
	database int
	prefix   string
	mu       sync.Mutex
	handlers map[string]func(*Task) (map[string]any, error)
	stop     chan struct{}
}

func NewRedis(addr, password string, database int, prefix string) *RedisQueue {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:6379"
	}
	if database < 0 {
		database = 0
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = "gptgrok2api"
	}
	return &RedisQueue{addr: addr, password: password, database: database, prefix: prefix, handlers: map[string]func(*Task) (map[string]any, error){}, stop: make(chan struct{})}
}

const (
	// redisOpTimeout 覆盖普通单次往返。缺失 deadline 会让 socket 无限等待，
	// Redis 假死时 worker 会被永久拖住。
	redisOpTimeout = 5 * time.Second
	// redisBlockTimeout 必须大于 BLPOP 的阻塞时长，否则连接会先于命令返回而超时。
	redisBlockTimeout = 8 * time.Second
)

// opCtx 为单次命令派生带超时的 context。
func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisOpTimeout)
}

// blockCtx 供 BLPOP/BRPOP 这类长阻塞命令使用。
func blockCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisBlockTimeout)
}

func (q *RedisQueue) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := q.command(ctx, "PING")
	return err
}

func (q *RedisQueue) Register(kind string, handler func(*Task) (map[string]any, error)) {
	q.mu.Lock()
	q.handlers[kind] = handler
	q.mu.Unlock()
}

func (q *RedisQueue) Start(workers int) {
	if workers < 1 {
		workers = 1
	}
	q.recoverRunning()
	for i := 0; i < workers; i++ {
		go q.worker()
	}
}

// Stop 让所有 worker 退出。重复调用安全。
func (q *RedisQueue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case <-q.stop:
	default:
		close(q.stop)
	}
}

// recoverRunning returns tasks whose worker disappeared before it could write
// a terminal state back to Redis. BLPOP removes a task from the queue, so a
// running task must be explicitly put back before workers start.
func (q *RedisQueue) recoverRunning() {
	ctx, cancel := opCtx()
	defer cancel()
	raw, err := q.command(ctx, "SMEMBERS", q.indexKey())
	if err != nil {
		return
	}
	items, ok := raw.([]any)
	if !ok {
		return
	}
	for _, value := range items {
		id := stringValue(rawBytes(value))
		if id == "" {
			continue
		}
		task, ok := q.Get(id)
		if !ok || task.Status != "running" {
			continue
		}
		task.Status = "queued"
		task.UpdatedAt = time.Now().Unix()
		if err := q.Save(&task); err != nil {
			log.Printf("recoverRunning: save task %s failed: %v", id, err)
		}
	}
}

func (q *RedisQueue) Submit(kind string, payload map[string]any) *Task {
	now := time.Now().Unix()
	task := &Task{ID: taskID(), Kind: kind, Status: "queued", Payload: payload, CreatedAt: now, UpdatedAt: now}
	if err := q.Save(task); err != nil {
		log.Printf("Submit: save task failed: %v", err)
	}
	return clone(task)
}

func (q *RedisQueue) Get(id string) (Task, bool) {
	ctx, cancel := opCtx()
	defer cancel()
	raw, err := q.command(ctx, "GET", q.taskKey(id))
	if err != nil || raw == nil {
		return Task{}, false
	}
	var task Task
	if json.Unmarshal(rawBytes(raw), &task) != nil {
		return Task{}, false
	}
	return *clone(&task), true
}

func (q *RedisQueue) Cancel(id string) bool {
	task, ok := q.Get(id)
	if !ok || task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
		return false
	}
	task.Status = "cancelled"
	task.UpdatedAt = time.Now().Unix()
	return q.Save(&task) == nil
}

func (q *RedisQueue) List() []Task {
	ctx, cancel := opCtx()
	defer cancel()
	raw, err := q.command(ctx, "SMEMBERS", q.indexKey())
	if err != nil {
		return []Task{}
	}
	items, _ := raw.([]any)
	result := make([]Task, 0, len(items))
	for _, value := range items {
		if task, ok := q.Get(stringValue(rawBytes(value))); ok {
			result = append(result, task)
		}
	}
	return result
}

func (q *RedisQueue) worker() {
	for {
		select {
		case <-q.stop:
			return
		default:
		}
		// BLPOP 使用 blockCtx 确保 timeout 可靠停止
		ctx, cancel := blockCtx()
		raw, err := q.command(ctx, "BLPOP", q.queueKey(), "5")
		cancel()
		if err != nil {
			select {
			case <-q.stop:
				return
			default:
				time.Sleep(time.Second)
				continue
			}
		}
		values, ok := raw.([]any)
		if !ok || len(values) < 2 {
			continue
		}
		id := stringValue(rawBytes(values[1]))
		task, ok := q.Get(id)
		if !ok || task.Status != "queued" {
			continue
		}
		task.Status = "running"
		task.UpdatedAt = time.Now().Unix()
		if err := q.Save(&task); err != nil {
			log.Printf("worker: save task %s running status failed: %v", id, err)
			continue
		}
		q.mu.Lock()
		handler := q.handlers[task.Kind]
		q.mu.Unlock()
		var result map[string]any
		var handlerErr error
		if handler == nil {
			handlerErr = errors.New("no task handler")
		} else {
			result, handlerErr = handler(&task)
		}
		current, exists := q.Get(id)
		if !exists || current.Status == "cancelled" {
			continue
		}
		current.UpdatedAt = time.Now().Unix()
		if handlerErr != nil {
			current.Status = "failed"
			current.Error = handlerErr.Error()
		} else {
			current.Status = "completed"
			current.Progress = 100
			current.Result = result
		}
		if err := q.Save(&current); err != nil {
			log.Printf("worker: save task %s final status failed: %v", id, err)
		}
	}
}

// Save 是 save 的公开包装，统一加超时。
// 原 save(t) 已被内部调用改为 Save(t)。
func (q *RedisQueue) Save(task *Task) error {
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	ctx, cancel := opCtx()
	defer cancel()
	if _, err := q.command(ctx, "SET", q.taskKey(task.ID), string(raw), "EX", "604800"); err != nil {
		return err
	}
	ctx2, cancel2 := opCtx()
	defer cancel2()
	_, _ = q.command(ctx2, "SADD", q.indexKey(), task.ID)
	if task.Status == "queued" {
		ctx3, cancel3 := opCtx()
		defer cancel3()
		_, _ = q.command(ctx3, "RPUSH", q.queueKey(), task.ID)
	}
	return nil
}

func (q *RedisQueue) taskKey(id string) string { return q.prefix + ":task:" + id }
func (q *RedisQueue) queueKey() string         { return q.prefix + ":tasks:queue" }
func (q *RedisQueue) indexKey() string         { return q.prefix + ":tasks:index" }

func (q *RedisQueue) command(ctx context.Context, args ...string) (any, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", q.addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if q.password != "" {
		if _, err := redisRoundTrip(conn, append([]string{"AUTH"}, q.password)); err != nil {
			return nil, err
		}
	}
	if q.database != 0 {
		if _, err := redisRoundTrip(conn, []string{"SELECT", strconv.Itoa(q.database)}); err != nil {
			return nil, err
		}
	}
	return redisRoundTrip(conn, args)
}

func redisRoundTrip(conn net.Conn, args []string) (any, error) {
	var builder strings.Builder
	builder.WriteString("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, arg := range args {
		builder.WriteString("$" + strconv.Itoa(len(arg)) + "\r\n" + arg + "\r\n")
	}
	if _, err := io.WriteString(conn, builder.String()); err != nil {
		return nil, err
	}
	return readRESP(bufio.NewReader(conn))
}

func readRESP(reader *bufio.Reader) (any, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, errors.New(line)
	case ':':
		value, _ := strconv.ParseInt(line, 10, 64)
		return value, nil
	case '$':
		length, _ := strconv.Atoi(line)
		if length < 0 {
			return nil, nil
		}
		data := make([]byte, length+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		return data[:length], nil
	case '*':
		length, _ := strconv.Atoi(line)
		if length < 0 {
			return nil, nil
		}
		items := make([]any, length)
		for i := range items {
			items[i], err = readRESP(reader)
			if err != nil {
				return nil, err
			}
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported Redis RESP type %q", prefix)
	}
}

func rawBytes(value any) []byte {
	switch typed := value.(type) {
	case []byte:
		return typed
	case string:
		return []byte(typed)
	default:
		return []byte(fmt.Sprint(value))
	}
}

func stringValue(value any) string { return strings.TrimSpace(string(rawBytes(value))) }
