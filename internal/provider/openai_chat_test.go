package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
	"github.com/auucoder/gptgrok2api-go/internal/protocol"
)

func TestOpenAIChatPayloadUsesRawMessageText(t *testing.T) {
	payload := openAIChatPayload(protocol.ChatRequest{
		Model: "gpt-5-6",
		Messages: []protocol.Message{
			{Role: "user", Content: "请回复Go测试成功"},
		},
	})
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("unexpected messages: %#v", payload["messages"])
	}
	message, _ := messages[0].(map[string]any)
	content, _ := message["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	if len(parts) != 1 || parts[0] != "请回复Go测试成功" {
		t.Fatalf("message text was decorated: %#v", parts)
	}
	if strings.Contains(parts[0].(string), "[user]") {
		t.Fatalf("message contains protocol decoration: %q", parts[0])
	}
}

func TestOpenAIChatStateReturnsSSEDetailAsError(t *testing.T) {
	state := &openAIChatState{}
	_, err := state.event(map[string]any{"detail": "Invalid conversation body"})
	if err == nil || !strings.Contains(err.Error(), "Invalid conversation body") {
		t.Fatalf("expected upstream detail error, got %v", err)
	}
}

func TestOpenAIChatPayloadSkipsEmptyMessageText(t *testing.T) {
	payload := openAIChatPayload(protocol.ChatRequest{
		Model: "auto",
		Messages: []protocol.Message{
			{Role: "system", Content: map[string]any{"non-text": true}},
			{Role: "user", Content: nil},
		},
	})
	messages, _ := payload["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("expected no messages for empty content, got %d", len(messages))
	}
}

func TestOpenAIChatPayloadJoinsStructuredTextParts(t *testing.T) {
	payload := openAIChatPayload(protocol.ChatRequest{
		Model: "gpt-5-5",
		Messages: []protocol.Message{
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "first part"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://x/y.png"}},
				map[string]any{"type": "text", "text": "second part"},
			}},
		},
	})
	messages, _ := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	content := messages[0].(map[string]any)["content"].(map[string]any)
	parts := content["parts"].([]any)
	if len(parts) != 1 || parts[0] != "first part\nsecond part" {
		t.Fatalf("unexpected joined parts: %#v", parts)
	}
}

func TestOpenAIChatPayloadDefaultsEmptyRoleAndModel(t *testing.T) {
	payload := openAIChatPayload(protocol.ChatRequest{
		Model:          "auto",
		Messages:       []protocol.Message{{Content: "hi"}},
		ReasoningEffort: "high",
	})
	messages, _ := payload["messages"].([]any)
	author, _ := messages[0].(map[string]any)["author"].(map[string]any)
	if author["role"] != "user" {
		t.Fatalf("default role = %q", author["role"])
	}
	if payload["model"] != "auto" {
		t.Fatalf("model = %q", payload["model"])
	}
	if payload["thinking_effort"] != "high" {
		t.Fatalf("thinking_effort = %q", payload["thinking_effort"])
	}
}

func TestOpenAIChatPayloadReasoningEffortNoneIsOmitted(t *testing.T) {
	payload := openAIChatPayload(protocol.ChatRequest{
		Messages:        []protocol.Message{{Content: "hi"}},
		ReasoningEffort: "none",
	})
	if _, present := payload["thinking_effort"]; present {
		t.Fatalf("thinking_effort should be omitted for none, got %#v", payload["thinking_effort"])
	}
}

func TestOpenAIChatMessageTextVariants(t *testing.T) {
	if got := openAIMessageText("plain"); got != "plain" {
		t.Fatalf("string text = %q", got)
	}
	// Non-map parts are skipped, so bare strings and numbers contribute nothing.
	if got := openAIMessageText([]any{"str", 42, map[string]any{"type": "text", "text": "part"}}); got != "part" {
		t.Fatalf("structured text = %q", got)
	}
	if got := openAIMessageText([]any{map[string]any{"type": "image_url", "text": "skip"}}); got != "" {
		t.Fatalf("non-text part = %q", got)
	}
	if got := openAIMessageText(nil); got != "" {
		t.Fatalf("nil text = %q", got)
	}
}

func TestOpenAIChatStreamEndToEnd(t *testing.T) {
	first := mustJSON(map[string]any{"message": map[string]any{"content": map[string]any{"parts": []any{"partial "}}}})
	second := mustJSON(map[string]any{"message": map[string]any{"content": map[string]any{"parts": []any{"text"}}}})
	body := fmt.Sprintf("data: %s\n\ndata: %s\n\nevent: ping\ndata: not-json\n\ndata: [DONE]\n\n", first, second)
	chat := newOpenAIChatTestServer(t, body)

	collected := []OpenAIChatEvent{}
	err := chat.Stream(context.Background(), accounts.Account{Token: "jwt.header.payload"}, protocol.ChatRequest{
		Messages: []protocol.Message{{Role: "user", Content: "hi"}},
	}, func(event OpenAIChatEvent) error {
		collected = append(collected, event)
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var full strings.Builder
	done := false
	for _, event := range collected {
		if event.Done {
			done = true
		}
		full.WriteString(event.Text)
	}
	if full.String() != "partial text" {
		t.Fatalf("assembled text = %q", full.String())
	}
	if !done {
		t.Fatal("expected a done event")
	}
}

func TestOpenAIChatStreamPropagatesOnEventError(t *testing.T) {
	body := "data: " + mustJSON(map[string]any{"message": map[string]any{"content": map[string]any{"parts": []any{"a "}}}}) + "\n\ndata: [DONE]\n\n"
	chat := newOpenAIChatTestServer(t, body)
	bail := errors.New("stop")
	calls := 0
	err := chat.Stream(context.Background(), accounts.Account{Token: "t"}, protocol.ChatRequest{
		Messages: []protocol.Message{{Content: "x"}},
	}, func(event OpenAIChatEvent) error {
		calls++
		return bail
	})
	if !errors.Is(err, bail) {
		t.Fatalf("expected onEvent error to propagate, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected onEvent to stop after first event, calls=%d", calls)
	}
}

func TestOpenAIChatCompleteEndToEnd(t *testing.T) {
	body := "data: " + mustJSON(map[string]any{"message": map[string]any{"content": map[string]any{"parts": []any{"done"}}}}) + "\n\ndata: [DONE]\n\n"
	chat := newOpenAIChatTestServer(t, body)
	text, thinking, err := chat.Complete(context.Background(), accounts.Account{Token: "t"}, protocol.ChatRequest{
		Messages: []protocol.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if text != "done" || thinking != "" {
		t.Fatalf("text=%q thinking=%q", text, thinking)
	}
}

func TestOpenAIChatStreamRequiresConfiguredProvider(t *testing.T) {
	err := NewOpenAIChat(nil).Stream(context.Background(), accounts.Account{}, protocol.ChatRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestOpenAIChatStreamSurfacesUpstreamError(t *testing.T) {
	body := "data: " + mustJSON(map[string]any{"error": map[string]any{"type": "rate_limit", "message": "too many"}}) + "\n\n"
	chat := newOpenAIChatTestServer(t, body)
	err := chat.Stream(context.Background(), accounts.Account{Token: "t"}, protocol.ChatRequest{
		Messages: []protocol.Message{{Content: "x"}},
	}, func(event OpenAIChatEvent) error { return nil })
	var upstream *protocol.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("expected upstream error, got %T: %v", err, err)
	}
	if upstream.Status != 429 {
		t.Fatalf("rate limit status = %d", upstream.Status)
	}
}

func TestOpenAIChatStreamSurfacesSentinelDetail(t *testing.T) {
	body := "data: " + mustJSON(map[string]any{"detail": "bad request"}) + "\n\n"
	chat := newOpenAIChatTestServer(t, body)
	err := chat.Stream(context.Background(), accounts.Account{Token: "t"}, protocol.ChatRequest{
		Messages: []protocol.Message{{Content: "x"}},
	}, func(event OpenAIChatEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("expected detail error, got %v", err)
	}
}

func TestOpenAIChatStreamDropsUnparsableLines(t *testing.T) {
	body := "data: broken\n\ndata: 42\n\ndata: {\"nothing\":true}\n\ndata: [DONE]\n\n"
	chat := newOpenAIChatTestServer(t, body)
	events := 0
	err := chat.Stream(context.Background(), accounts.Account{Token: "t"}, protocol.ChatRequest{
		Messages: []protocol.Message{{Content: "x"}},
	}, func(event OpenAIChatEvent) error {
		if event.Text != "" {
			t.Errorf("unexpected text event: %+v", event)
		}
		events++
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if events != 1 {
		t.Fatalf("expected only the done event, got %d", events)
	}
}

func TestAppendDeltaVariants(t *testing.T) {
	current := ""
	if delta := appendDelta(&current, ""); delta != "" {
		t.Fatalf("empty candidate delta = %q", delta)
	}
	if delta := appendDelta(&current, "hello"); delta != "hello" {
		t.Fatalf("first delta = %q", delta)
	}
	if delta := appendDelta(&current, "hello world"); delta != " world" {
		t.Fatalf("extension delta = %q", delta)
	}
	if delta := appendDelta(&current, "hello wor"); delta != "" {
		t.Fatalf("shorter candidate should be ignored, delta = %q", delta)
	}
	// A candidate that shares no prefix is treated as a fresh fragment and
	// concatenated onto the accumulator rather than replacing it.
	if delta := appendDelta(&current, "unrelated"); delta != "unrelated" {
		t.Fatalf("divergent delta = %q", delta)
	}
	if current != "hello worldunrelated" {
		t.Fatalf("current = %q", current)
	}
}

func TestOpenAIChatStateEventNonObject(t *testing.T) {
	state := &openAIChatState{}
	event, err := state.event("not-an-object")
	if err != nil || event.Text != "" || event.Done {
		t.Fatalf("unexpected event for non-object: %+v err=%v", event, err)
	}
}

func TestOpenAIChatStateEventAppendText(t *testing.T) {
	state := &openAIChatState{}
	event, err := state.event(map[string]any{"v": "hello", "o": "append"})
	if err != nil || event.Text != "hello" {
		t.Fatalf("unexpected append event: %+v err=%v", event, err)
	}
	if state.text != "hello" {
		t.Fatalf("state text = %q", state.text)
	}
}

func TestOpenAIChatStateEventCompletionFlags(t *testing.T) {
	for _, flag := range []string{"is_complete", "finished_successfully"} {
		state := &openAIChatState{}
		event, err := state.event(map[string]any{flag: true})
		if err != nil {
			t.Fatalf("%s parse: %v", flag, err)
		}
		if !event.Done {
			t.Fatalf("expected done for %s", flag)
		}
	}
}

func TestOpenAIChatStateEventNonRateErrorUsesBadGateway(t *testing.T) {
	state := &openAIChatState{}
	_, err := state.event(map[string]any{"error": map[string]any{"code": "server_error", "error": "boom"}})
	var upstream *protocol.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("expected upstream error, got %T", err)
	}
	if upstream.Status != 502 {
		t.Fatalf("non-rate status = %d", upstream.Status)
	}
}

func TestOpenAIChatStateEmitsOnlyIncrementalText(t *testing.T) {
	state := &openAIChatState{}
	first, err := state.event(map[string]any{"message": map[string]any{"content": map[string]any{"parts": []any{"Hel"}}}})
	if err != nil || first.Text != "Hel" {
		t.Fatalf("first event = %+v err=%v", first, err)
	}
	second, err := state.event(map[string]any{"message": map[string]any{"content": map[string]any{"parts": []any{"Hello world"}}}})
	if err != nil || second.Text != "lo world" {
		t.Fatalf("cumulative stream should emit only the delta, got %+v err=%v", second, err)
	}
}

// newOpenAIChatTestServer wires the minimal ChatGPT web surface the chat
// provider touches before the SSE conversation endpoint.
func newOpenAIChatTestServer(t *testing.T, streamBody string) *OpenAIChat {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<html data-build="test-build"><script src="/static/app.js"></script></html>`))
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"prepare_token": "prepare-token"})
		case "/backend-api/sentinel/chat-requirements/finalize":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "requirements-token"})
		case "/backend-api/conversation":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return NewOpenAIChat(NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second))
}
