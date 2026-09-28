package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
)

func TestOpenAIAssistantText(t *testing.T) {
	parts := map[string]any{
		"message": map[string]any{
			"author":  map[string]any{"role": "assistant"},
			"content": map[string]any{"parts": []any{"Hello ", "world"}},
		},
	}
	if got := openAIAssistantText(parts); got != "Hello world" {
		t.Fatalf("parts text = %q", got)
	}

	wrapped := map[string]any{"v": map[string]any{
		"message": map[string]any{"content": map[string]any{"text": "wrapped"}},
	}}
	if got := openAIAssistantText(wrapped); got != "wrapped" {
		t.Fatalf("wrapped text = %q", got)
	}

	skipped := map[string]any{"message": map[string]any{
		"author":  map[string]any{"role": "user"},
		"content": map[string]any{"parts": []any{"nope"}},
	}}
	if got := openAIAssistantText(skipped); got != "" {
		t.Fatalf("user message should be skipped, got %q", got)
	}

	if got := openAIAssistantText(map[string]any{"message": "not a map"}); got != "" {
		t.Fatalf("non-map message = %q", got)
	}
}

func TestAppendDelta(t *testing.T) {
	current := "Hello"
	if got := appendDelta(&current, "Hello world"); got != " world" {
		t.Fatalf("extension delta = %q", got)
	}
	if current != "Hello world" {
		t.Fatalf("current not advanced: %q", current)
	}
	if got := appendDelta(&current, "Hello world"); got != "" {
		t.Fatalf("repeat should yield empty, got %q", got)
	}
	if got := appendDelta(&current, ""); got != "" {
		t.Fatalf("empty candidate = %q", got)
	}
	if got := appendDelta(&current, "brand new"); got != "brand new" {
		t.Fatalf("divergent text = %q", got)
	}
	if current != "Hello worldbrand new" {
		t.Fatalf("append result = %q", current)
	}
}

func TestHexBytes(t *testing.T) {
	got, err := hexBytes("00ff10")
	if err != nil {
		t.Fatalf("hexBytes: %v", err)
	}
	if len(got) != 3 || got[0] != 0x00 || got[1] != 0xff || got[2] != 0x10 {
		t.Fatalf("decoded = %v", got)
	}
	if _, err := hexBytes("abc"); err == nil {
		t.Fatal("odd length should error")
	}
	if _, err := hexBytes("zz"); err == nil {
		t.Fatal("non-hex should error")
	}
}

func TestBuildProofTokenInvalidDifficulty(t *testing.T) {
	if got := buildProofToken("seed", "abc", "ua", nil, "build-1"); got != "" {
		t.Fatalf("odd difficulty should return empty, got %q", got)
	}
}

func TestToAnyListAndNumberSentinel(t *testing.T) {
	list := []any{1, 2}
	if got := toAnyList(list); len(got) != 2 {
		t.Fatalf("toAnyList passthrough = %v", got)
	}
	if got := toAnyList("nope"); got != nil {
		t.Fatalf("toAnyList non-list = %v", got)
	}

	if got := numberSentinel(float64(2.5)); got != 2.5 {
		t.Fatalf("numberSentinel float = %v", got)
	}
	if got := numberSentinel(3); got != 3 {
		t.Fatalf("numberSentinel int = %v", got)
	}
	if got := numberSentinel("4.5"); got != 4.5 {
		t.Fatalf("numberSentinel string = %v", got)
	}
	if got := numberSentinel("bad"); got != 0 {
		t.Fatalf("numberSentinel bad string = %v", got)
	}
	if got := numberSentinel(nil); got != 0 {
		t.Fatalf("numberSentinel nil = %v", got)
	}
}

func TestEqualSentinel(t *testing.T) {
	if !equalSentinel(float64(3), 3) {
		t.Fatal("numeric cross-type equality should hold")
	}
	if !equalSentinel("a", "a") {
		t.Fatal("string equality should hold")
	}
	if equalSentinel("a", "b") {
		t.Fatal("different strings should not be equal")
	}
}

func TestSentinelJSON(t *testing.T) {
	if got := sentinelJSON(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Fatalf("sentinelJSON = %q", got)
	}
	if got := sentinelJSON(make(chan int)); got != "null" {
		t.Fatalf("unmarshalable should be null, got %q", got)
	}
}

func TestSentinelVMGetAndToString(t *testing.T) {
	vm := &sentinelVM{values: map[any]any{"k": "v"}}
	if got := vm.get("k"); got != "v" {
		t.Fatalf("get = %v", got)
	}
	if got := vm.get("missing"); got != nil {
		t.Fatalf("missing get = %v", got)
	}

	if got := vm.toString(nil); got != "undefined" {
		t.Fatalf("nil toString = %q", got)
	}
	if got := vm.toString("text"); got != "text" {
		t.Fatalf("string toString = %q", got)
	}
	if got := vm.toString(float64(5)); got != "5.0" {
		t.Fatalf("integral float toString = %q", got)
	}
	if got := vm.toString(2.25); got != "2.25" {
		t.Fatalf("fractional float toString = %q", got)
	}
	if got := vm.toString(true); got != "true" {
		t.Fatalf("bool true toString = %q", got)
	}
	if got := vm.toString(false); got != "false" {
		t.Fatalf("bool false toString = %q", got)
	}
}

func TestSentinelVMQueue(t *testing.T) {
	vm := &sentinelVM{values: map[any]any{float64(9): []any{"a"}}}
	if got := vm.queue(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("queue = %v", got)
	}
	empty := &sentinelVM{values: map[any]any{}}
	if got := empty.queue(); got != nil {
		t.Fatalf("empty queue = %v", got)
	}
}

func TestGrokChatDo(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	chat := NewGrokChat(server.URL, server.Client(), 5*time.Second)
	response, err := chat.Do(context.Background(), accounts.Account{Token: "sso=tok"}, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	if received != "sso=tok; sso-rw=tok" {
		t.Fatalf("cookie header = %q", received)
	}
}

func TestConsoleChatDo(t *testing.T) {
	var cluster string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cluster = r.Header.Get("x-cluster")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	chat := NewConsoleChat(server.URL, server.Client())
	response, err := chat.Do(context.Background(), accounts.Account{Token: "sso=tok"}, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	if cluster != "https://us-east-1.api.x.ai" {
		t.Fatalf("x-cluster = %q", cluster)
	}
}

func TestSetProxyManagerNoop(t *testing.T) {
	// setters must tolerate nil managers without panicking
	NewGrokChat("", nil, 0).SetProxyManager(nil)
	NewConsoleChat("", nil).SetProxyManager(nil)
	NewMedia(nil, "", "", "", "", 0).SetProxyManager(nil)
}
