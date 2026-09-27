package provider

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
	"github.com/auucoder/gptgrok2api-go/internal/proxy"
)

func sendWSFrame(conn net.Conn, opcode byte, payload string) error {
	data := []byte(payload)
	header := []byte{0x80 | opcode}
	length := len(data)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(length))
		header = append(header, buf[:]...)
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

func wsHandshake(conn net.Conn, br *bufio.Reader) error {
	req, err := http.ReadRequest(br)
	if err != nil {
		return err
	}
	return wsHandshakeReq(conn, req)
}

func wsHandshakeReq(conn net.Conn, req *http.Request) error {
	key := req.Header.Get("Sec-WebSocket-Key")
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	handshake := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"
	_, err := io.WriteString(conn, handshake)
	return err
}

func readClientWSFrame(br *bufio.Reader) ([]byte, byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(br, header); err != nil {
		return nil, 0, err
	}
	masked := header[1]&0x80 != 0
	length := int64(header[1] & 0x7f)
	switch {
	case length == 126:
		var buf [2]byte
		if _, err := io.ReadFull(br, buf[:]); err != nil {
			return nil, 0, err
		}
		length = int64(binary.BigEndian.Uint16(buf[:]))
	case length == 127:
		var buf [8]byte
		if _, err := io.ReadFull(br, buf[:]); err != nil {
			return nil, 0, err
		}
		length = int64(binary.BigEndian.Uint64(buf[:]))
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, 0, err
	}
	if masked {
		var mask [4]byte
		if _, err := io.ReadFull(br, mask[:]); err != nil {
			return nil, 0, err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, header[0] & 0x0f, nil
}

func imageResultJSON(id string) string {
	blob := base64.StdEncoding.EncodeToString([]byte("fake-image-" + id))
	msg := map[string]any{"type": "image", "url": "https://assets.grok.com/" + id + ".jpg", "blob": blob}
	raw, _ := json.Marshal(msg)
	return string(raw)
}

func TestImagineSocketGenerateHappyPath(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var cookieCapture string
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		req, _ := http.ReadRequest(br)
		cookieCapture = req.Header.Get("Cookie")
		if err := wsHandshakeReq(conn, req); err != nil {
			return
		}
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"started"}`)
		sendWSFrame(conn, 0x1, imageResultJSON("img1"))
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	results, err := ws.Generate(context.Background(), accounts.Account{Token: "sso=test"}, "cat", "16:9", 1, false, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 image, got %d", len(results))
	}
	if results[0].MIME != "image/jpeg" {
		t.Fatalf("MIME = %s", results[0].MIME)
	}
	if !strings.Contains(results[0].URL, "img1") {
		t.Fatalf("URL missing img1: %s", results[0].URL)
	}
	if cookieCapture != "sso=test; sso-rw=test" {
		t.Fatalf("cookie = %s", cookieCapture)
	}
}

func TestImagineSocketGenerateErrorFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, `{"type":"error","err_msg":"server boom"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil || !strings.Contains(err.Error(), "server boom") {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestImagineSocketGenerateNoImagesTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		time.Sleep(2 * time.Second)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 100*time.Millisecond)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestImagineSocketInvalidScheme(t *testing.T) {
	ws := NewImagineSocket("http://x/imagine", &http.Client{}, time.Second)
	_, err := ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil || !strings.Contains(err.Error(), "invalid imagine websocket URL") {
		t.Fatalf("expected scheme error, got %v", err)
	}
}

func TestImagineSocketBadHandshakeAccept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		http.ReadRequest(br)
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: bad\r\n\r\n")
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil || !strings.Contains(err.Error(), "invalid websocket handshake") {
		t.Fatalf("expected handshake error, got %v", err)
	}
}

func TestImagineSocketNonUpgradeResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		http.ReadRequest(br)
		io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil || !strings.Contains(err.Error(), "handshake returned HTTP") {
		t.Fatalf("expected HTTP status error, got %v", err)
	}
}

func TestImagineSocketDuplicateImageSuppressed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, imageResultJSON("dup"))
		sendWSFrame(conn, 0x1, imageResultJSON("dup"))
		sendWSFrame(conn, 0x1, imageResultJSON("other"))
		sendWSFrame(conn, 0x1, imageResultJSON("third"))
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	results, err := ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 3, false, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 unique images, got %d", len(results))
	}
}

func TestImagineSocketEmptyBlobSkipped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, `{"type":"image","url":"https://x/a.jpg","blob":""}`)
		sendWSFrame(conn, 0x1, imageResultJSON("real"))
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	results, err := ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 image, got %d", len(results))
	}
}

func TestImagineSocketExplicitCookieHeader(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var gotCookie string
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		req, _ := http.ReadRequest(br)
		gotCookie = req.Header.Get("Cookie")
		wsHandshakeReq(conn, req)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, imageResultJSON("c"))
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	_, err = ws.Generate(context.Background(), accounts.Account{Fields: map[string]any{"cookie_header": "explicit"}}, "p", "", 1, false, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotCookie != "explicit" {
		t.Fatalf("expected explicit cookie, got %s", gotCookie)
	}
}

func TestImagineSocketInvalidBase64Blob(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, `{"type":"image","url":"https://x/bad.jpg","blob":"!!!not-b64!!!"}`)
		sendWSFrame(conn, 0x1, imageResultJSON("ok"))
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	results, err := ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(results) != 1 || results[0].Base64 != "" {
		t.Fatalf("expected empty Base64 on decode failure, got %+v", results)
	}
}

func TestImagineSocketProxyBranch(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		conn.Close()
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	ws.Proxy = proxy.NewManager("", nil)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil {
		t.Fatal("expected error from proxy branch")
	}
}

func TestImagineSocketWSSConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		conn.Close()
	}()
	defer ln.Close()

	ws := NewImagineSocket("wss://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil {
		t.Fatal("expected TLS handshake error")
	}
}

func TestNewImagineSocketNilClient(t *testing.T) {
	ws := NewImagineSocket("ws://x/imagine", nil, 5*time.Second)
	if ws.Client == nil {
		t.Fatal("expected non-nil client")
	}
	if ws.Client.Timeout != 5*time.Second {
		t.Fatalf("timeout = %v", ws.Client.Timeout)
	}
}

func TestImagineSocketModeratedJsonSkipped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed","moderated":true}`)
		time.Sleep(2 * time.Second)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 100*time.Millisecond)
	_, err = ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestWsConnWriteFrameLongPayload(t *testing.T) {
	w := &wsConn{conn: &nopConn{}}
	payload := strings.Repeat("A", 300)
	if err := w.writeFrame(0x1, []byte(payload)); err != nil {
		t.Fatalf("writeFrame long: %v", err)
	}
}

type nopConn struct {
	mu sync.Mutex
	buf []byte
}

func (n *nopConn) Write(p []byte) (int, error) {
	n.mu.Lock()
	n.buf = append(n.buf, p...)
	n.mu.Unlock()
	return len(p), nil
}
func (n *nopConn) Read(p []byte) (int, error)         { return 0, io.EOF }
func (n *nopConn) Close() error                         { return nil }
func (n *nopConn) LocalAddr() net.Addr                  { return nil }
func (n *nopConn) RemoteAddr() net.Addr                 { return nil }
func (n *nopConn) SetDeadline(t time.Time) error        { return nil }
func (n *nopConn) SetReadDeadline(t time.Time) error    { return nil }
func (n *nopConn) SetWriteDeadline(t time.Time) error   { return nil }

// tcpPair returns two connected TCP endpoints. Unlike net.Pipe they buffer,
// so a reader replying (pong frames) never deadlocks against a writer.
func tcpPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- conn
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-accepted
	if server == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return client, server
}

func TestReadTextHandlesPingAndBinary(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		sendWSFrame(b, 0x9, "ping")
		sendWSFrame(b, 0x2, "binary")
		sendWSFrame(b, 0x1, "hello")
	}()
	got, err := w.readText(context.Background(), time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatalf("readText: %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
}

func TestReadTextCloseFrame(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		sendWSFrame(b, 0x8, "")
	}()
	_, err := w.readText(context.Background(), time.Now().Add(2*time.Second))
	if err != io.EOF {
		t.Fatalf("expected EOF on close, got %v", err)
	}
}

func TestReadTextLargeFrame(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		sendWSFrame(b, 0x1, strings.Repeat("x", 200))
	}()
	got, err := w.readText(context.Background(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("readText large: %v", err)
	}
	if len(got) != 200 {
		t.Fatalf("expected 200 bytes, got %d", len(got))
	}
}

func TestReadTextFrameTooLarge(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(40<<20))
		_, _ = b.Write([]byte{0x81, 0x80 | 127})
		_, _ = b.Write(buf[:])
	}()
	_, err := w.readText(context.Background(), time.Now().Add(2*time.Second))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too large error, got %v", err)
	}
}

func TestReadText126Length(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		payload := strings.Repeat("z", 200)
		header := []byte{0x81, 126}
		var lenBytes [2]byte
		binary.BigEndian.PutUint16(lenBytes[:], uint16(len(payload)))
		_, _ = b.Write(append(header, lenBytes[:]...))
		_, _ = b.Write([]byte(payload))
	}()
	got, err := w.readText(context.Background(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("readText 126: %v", err)
	}
	if len(got) != 200 {
		t.Fatalf("expected 200, got %d", len(got))
	}
}

func TestImagineSocketGenerateContinuesOnNonTextOpcode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		br := bufio.NewReader(conn)
		wsHandshake(conn, br)
		readClientWSFrame(br)
		readClientWSFrame(br)
		// Send binary frame (should be skipped)
		sendWSFrame(conn, 0x2, "binary-ignore")
		sendWSFrame(conn, 0x1, imageResultJSON("after-binary"))
		sendWSFrame(conn, 0x1, `{"type":"json","current_status":"completed"}`)
	}()
	defer ln.Close()

	ws := NewImagineSocket("ws://"+ln.Addr().String()+"/imagine", &http.Client{}, 2*time.Second)
	results, err := ws.Generate(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 image after binary, got %d", len(results))
	}
}

func TestGenerateImagineViaMedia(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		req, _ := http.ReadRequest(br)
		wsHandshakeReq(conn, req)
		readClientWSFrame(br)
		readClientWSFrame(br)
		sendWSFrame(conn, 0x1, imageResultJSON("via-media"))
	}()

	media := NewMedia(&http.Client{}, "", "", "", "", 2*time.Second)
	results, err := media.GenerateImagine(context.Background(), accounts.Account{Token: "sso=media"}, "cat", "1:1", 1, false, false, "ws://"+ln.Addr().String()+"/imagine")
	if err != nil {
		t.Fatalf("GenerateImagine: %v", err)
	}
	if len(results) != 1 || !strings.Contains(results[0].URL, "via-media") {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestGenerateImagineBadURL(t *testing.T) {
	media := NewMedia(&http.Client{}, "", "", "", "", time.Second)
	if _, err := media.GenerateImagine(context.Background(), accounts.Account{Token: "sso=x"}, "p", "", 1, false, false, "ftp://bad"); err == nil {
		t.Fatal("expected error for non websocket URL")
	}
}

func TestWsConnWriteFrame127Length(t *testing.T) {
	w := &wsConn{conn: &nopConn{}}
	if err := w.writeFrame(0x1, make([]byte, 70000)); err != nil {
		t.Fatalf("writeFrame 127: %v", err)
	}
}

func TestWsConnWriteFramePropagatesWriteError(t *testing.T) {
	w := &wsConn{conn: &failingConn{}}
	if err := w.writeFrame(0x1, []byte("x")); err == nil {
		t.Fatal("expected write error")
	}
}

type failingConn struct{ nopConn }

func (f *failingConn) Write(p []byte) (int, error) { return 0, errors.New("write failed") }

func TestReadTextMaskedFrame(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		// masked client-style frame: the client must unmask it
		mask := []byte{0xde, 0xad, 0xbe, 0xef}
		payload := []byte("masked-payload")
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		header := []byte{0x81, 0x80 | byte(len(payload))}
		header = append(header, mask...)
		_, _ = b.Write(append(header, payload...))
	}()
	got, err := w.readText(context.Background(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("readText masked: %v", err)
	}
	if got != "masked-payload" {
		t.Fatalf("got %q", got)
	}
}

func TestReadTextSkipsContinuation(t *testing.T) {
	a, b := tcpPair(t)
	w := &wsConn{conn: a, read: bufio.NewReader(a)}
	go func() {
		// fin=0 fragment, then a binary control-ish frame, then a final text frame
		_, _ = b.Write([]byte{0x01, 0x03, 'a', 'b', 'c'})
		_, _ = b.Write([]byte{0x82, 0x01, 0x00})
		_, _ = b.Write([]byte{0x80 | 0x1, 0x02, 'o', 'k'})
	}()
	got, err := w.readText(context.Background(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("readText continuation: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want ok", got)
	}
}

func TestReadTextSetReadDeadlineError(t *testing.T) {
	w := &wsConn{conn: &deadlineFailConn{}, read: bufio.NewReader(&nopConn{})}
	if _, err := w.readText(context.Background(), time.Now().Add(time.Second)); err == nil {
		t.Fatal("expected deadline error")
	}
}

type deadlineFailConn struct{ nopConn }

func (d *deadlineFailConn) SetReadDeadline(t time.Time) error { return errors.New("deadline failed") }
