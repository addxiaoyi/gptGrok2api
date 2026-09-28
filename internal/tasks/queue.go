package tasks

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Task struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Status    string         `json:"status"`
	Progress  int            `json:"progress"`
	Payload   map[string]any `json:"payload,omitempty"`
	Result    map[string]any `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	CreatedAt int64          `json:"created_at"`
	UpdatedAt int64          `json:"updated_at"`
}

type Queue struct {
	path     string
	mu       sync.Mutex
	items    map[string]*Task
	wake     chan struct{}
	handlers map[string]func(*Task) (map[string]any, error)
	stop     chan struct{}
	stopped  bool
}

type QueueAPI interface {
	Register(string, func(*Task) (map[string]any, error))
	Start(int)
	Stop()
	Submit(string, map[string]any) *Task
	Get(string) (Task, bool)
	Cancel(string) bool
	List() []Task
}

func New(path string) *Queue {
	q := &Queue{path: path, items: map[string]*Task{}, wake: make(chan struct{}, 1), stop: make(chan struct{}), handlers: map[string]func(*Task) (map[string]any, error){}}
	_ = q.load()
	return q
}

func (q *Queue) Register(kind string, handler func(*Task) (map[string]any, error)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[kind] = handler
	q.signal()
}

func (q *Queue) Start(workers int) {
	if workers < 1 {
		workers = 1
	}
	for index := 0; index < workers; index++ {
		go q.worker()
	}
	q.signal()
}

// Stop 让所有 worker 退出。重复调用安全。
func (q *Queue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return
	}
	q.stopped = true
	close(q.stop)
}

func (q *Queue) Submit(kind string, payload map[string]any) *Task {
	now := time.Now().Unix()
	task := &Task{
		ID:        taskID(),
		Kind:      kind,
		Status:    "queued",
		Payload:   payload,
		CreatedAt: now,
		UpdatedAt: now,
	}
	q.mu.Lock()
	q.items[task.ID] = task
	_ = q.saveLocked()
	cloned := clone(task)
	q.mu.Unlock()
	q.signal()
	return cloned
}

func (q *Queue) Get(id string) (Task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.items[id]
	if !ok {
		return Task{}, false
	}
	return *clone(task), true
}

func (q *Queue) Cancel(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.items[id]
	if !ok {
		return false
	}
	if task.Status == "completed" || task.Status == "failed" {
		return false
	}
	task.Status = "cancelled"
	task.UpdatedAt = time.Now().Unix()
	_ = q.saveLocked()
	return true
}

func (q *Queue) List() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]Task, 0, len(q.items))
	for _, task := range q.items {
		result = append(result, *clone(task))
	}
	return result
}

func (q *Queue) worker() {
	for {
		q.mu.Lock()
		var selected *Task
		for _, task := range q.items {
			if task.Status == "queued" {
				if selected == nil || task.CreatedAt < selected.CreatedAt {
					selected = task
				}
			}
		}
		var job *Task
		if selected != nil {
			selected.Status = "running"
			selected.UpdatedAt = time.Now().Unix()
			job = clone(selected)
			_ = q.saveLocked()
		}
		handler := func(*Task) (map[string]any, error) { return nil, errors.New("no task handler") }
		if job != nil {
			if current, ok := q.handlers[job.Kind]; ok {
				handler = current
			}
		}
		q.mu.Unlock()
		if job == nil {
			select {
			case <-q.stop:
				return
			case <-q.wake:
			}
			continue
		}
		// handler 只拿克隆副本，避免与 queue 内的活对象并发读写
		result, err := handler(job)
		q.mu.Lock()
		if current, ok := q.items[job.ID]; ok && current.Status == "running" {
			current.UpdatedAt = time.Now().Unix()
			if err != nil {
				current.Status = "failed"
				current.Error = err.Error()
			} else {
				current.Status = "completed"
				current.Progress = 100
				current.Result = result
			}
			_ = q.saveLocked()
		}
		q.mu.Unlock()
	}
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *Queue) load() error {
	raw, err := os.ReadFile(q.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var items []Task
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	for _, task := range items {
		if task.Status == "running" {
			task.Status = "queued"
		}
		q.items[task.ID] = clone(&task)
	}
	return nil
}
func (q *Queue) saveLocked() error {
	items := make([]Task, 0, len(q.items))
	for _, task := range q.items {
		items = append(items, *task)
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(q.path), ".tasks-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = tmp.Write(raw)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, q.path)
}
func clone(task *Task) *Task {
	copy := *task
	if task.Payload != nil {
		copy.Payload = map[string]any{}
		for key, value := range task.Payload {
			copy.Payload[key] = value
		}
	}
	if task.Result != nil {
		copy.Result = map[string]any{}
		for key, value := range task.Result {
			copy.Result[key] = value
		}
	}
	return &copy
}
func taskID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "task_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "task_" + base64.RawURLEncoding.EncodeToString(raw)
}
