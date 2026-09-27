package protocol

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestBuildConsolePayloadMapsAliasAndReasoning(t *testing.T) {
	payload, err := BuildConsolePayload(
		[]Message{{Role: "developer", Content: "be brief"}, {Role: "", Content: "hi"}},
		"grok-4.3-high", "", nil, nil, nil, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "grok-4.3" {
		t.Fatalf("expected alias to resolve to grok-4.3, got %#v", payload["model"])
	}
	// high 是固定档位，忽略请求里的 reasoning_effort。
	reasoning := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("expected fixed high effort, got %#v", reasoning["effort"])
	}
	input := payload["input"].([]map[string]any)
	if len(input) != 2 {
		t.Fatalf("expected 2 input messages, got %d", len(input))
	}
	if input[0]["role"] != "system" || input[1]["role"] != "user" {
		t.Fatalf("unexpected roles: %#v", input)
	}
}

func TestBuildConsolePayloadSkipsEmptyContent(t *testing.T) {
	payload, err := BuildConsolePayload(
		[]Message{{Role: "user", Content: "   "}, {Role: "user", Content: "real"}},
		"grok-4.20-0309-console", "low", floatPtr(0.1), floatPtr(0.5), nil, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload["input"].([]map[string]any)) != 1 {
		t.Fatal("expected blank content to be dropped")
	}
	if payload["temperature"] != 0.1 || payload["top_p"] != 0.5 {
		t.Fatalf("expected explicit sampling values, got %#v", payload)
	}
}

func TestBuildConsolePayloadRejectsEmptyInput(t *testing.T) {
	if _, err := BuildConsolePayload(nil, "grok-4.3", "", nil, nil, nil, false); err == nil {
		t.Fatal("expected empty input to error")
	}
}

func TestBuildConsolePayloadRejectsNonPositiveMaxTokens(t *testing.T) {
	if _, err := BuildConsolePayload(
		[]Message{{Role: "user", Content: "hi"}}, "grok-4.3", "", nil, nil, intPtr(0), false,
	); err == nil {
		t.Fatal("expected non-positive max_output_tokens to error")
	}
}

func TestBuildConsolePayloadMaxTokensPerModel(t *testing.T) {
	cases := map[string]int{
		"grok-4.20-multi-agent-xhigh": 2000000,
		"grok-build-console":          256000,
		"grok-4.3":                    1000000,
	}
	for model, want := range cases {
		payload, err := BuildConsolePayload([]Message{{Role: "user", Content: "hi"}}, model, "", nil, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if payload["max_output_tokens"] != want {
			t.Errorf("model %s: expected %d, got %#v", model, want, payload["max_output_tokens"])
		}
	}
}

func TestBuildConsolePayloadEffortFallbacks(t *testing.T) {
	cases := map[string]string{
		"minimal":  "low",
		"xhigh":    "xhigh",
		"":         "medium",
		"unknown":  "medium",
		"none":     "none",
		"medium":   "medium",
	}
	for input, want := range cases {
		payload, err := BuildConsolePayload(
			[]Message{{Role: "user", Content: "hi"}}, "grok-4.3", input, nil, nil, nil, false,
		)
		if err != nil {
			t.Fatal(err)
		}
		reasoning, ok := payload["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("grok-4.3 should carry reasoning block, got %#v", payload)
		}
		if reasoning["effort"] != want {
			t.Errorf("effort %q: expected %q, got %#v", input, want, reasoning["effort"])
		}
	}
}

func TestBuildConsolePayloadNoReasoningForNonReasoningModels(t *testing.T) {
	payload, err := BuildConsolePayload(
		[]Message{{Role: "user", Content: "hi"}}, "grok-4.20-0309-non-reasoning-console", "", nil, nil, nil, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["reasoning"]; ok {
		t.Fatal("non-reasoning model should not carry a reasoning block")
	}
}

func TestBuildConsolePayloadEnablesSearchTools(t *testing.T) {
	payload, err := BuildConsolePayload(
		[]Message{{Role: "user", Content: "hi"}}, "grok-4.20-0309-console", "", nil, nil, nil, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := payload["tools"].([]map[string]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("expected web_search + x_search tools, got %#v", payload["tools"])
	}
	if payload["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", payload["tool_choice"])
	}
}

func TestBuildConsolePayloadUsesDefaultsForSampling(t *testing.T) {
	payload, err := BuildConsolePayload([]Message{{Role: "user", Content: "hi"}}, "grok-4.3", "", nil, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.7 || payload["top_p"] != 0.95 {
		t.Fatalf("expected default sampling values, got %#v", payload)
	}
}

func TestConsoleContentConvertsBlocks(t *testing.T) {
	blocks := consoleContent([]any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://a/b.png"}},
		map[string]any{"type": "input_image", "image_url": "https://a/c.png"},
		map[string]any{"type": "image_url", "image_url": ""},
		"skipped",
	})
	if len(blocks) != 3 {
		t.Fatalf("expected 3 usable blocks, got %#v", blocks)
	}
	if blocks[0]["text"] != "hello" || blocks[0]["type"] != "input_text" {
		t.Fatalf("unexpected text block: %#v", blocks[0])
	}
	if blocks[1]["image_url"] != "https://a/b.png" || blocks[1]["type"] != "input_image" {
		t.Fatalf("unexpected nested image block: %#v", blocks[1])
	}
}

func TestConsoleContentRejectsBlankAndUnknown(t *testing.T) {
	if consoleContent("  ") != nil {
		t.Fatal("expected blank string to produce no blocks")
	}
	if consoleContent(42) != nil {
		t.Fatal("expected unsupported type to produce no blocks")
	}
}

func TestParseConsoleLine(t *testing.T) {
	if kind, value := ParseConsoleLine("  event: response.completed "); kind != "event" || value != "response.completed" {
		t.Fatalf("unexpected event parse: %q %q", kind, value)
	}
	if kind, value := ParseConsoleLine("data: [DONE]"); kind != "done" || value != "" {
		t.Fatalf("unexpected done parse: %q %q", kind, value)
	}
	if kind, value := ParseConsoleLine("data: {\"a\":1}"); kind != "data" || value != `{"a":1}` {
		t.Fatalf("unexpected data parse: %q %q", kind, value)
	}
	if kind, _ := ParseConsoleLine("garbage"); kind != "skip" {
		t.Fatalf("expected skip for non-SSE line, got %q", kind)
	}
}

func TestDecodeConsoleEventDeltaAndCompleted(t *testing.T) {
	event, err := decodeConsoleEvent("response.output_text.delta", `{"delta":"chunk"}`)
	if err != nil || event.Delta != "chunk" {
		t.Fatalf("unexpected delta event: %#v %v", event, err)
	}
	event, err = decodeConsoleEvent("response.completed", `{"response":{"usage":{"input_tokens":7}}}`)
	if err != nil || !event.Done || event.Usage["input_tokens"] != float64(7) {
		t.Fatalf("unexpected completed event: %#v %v", event, err)
	}
}

func TestDecodeConsoleEventIgnoresMalformedJSON(t *testing.T) {
	event, err := decodeConsoleEvent("response.output_text.delta", "{oops")
	if err != nil || event.Type != "" {
		t.Fatalf("expected malformed JSON to yield empty event, got %#v %v", event, err)
	}
}

func TestDecodeConsoleEventErrorShapes(t *testing.T) {
	event, _ := decodeConsoleEvent("error", `{"error":{"message":"boom"}}`)
	if event.Err == nil || event.Err.Status != 502 || event.Err.Message != "boom" {
		t.Fatalf("unexpected error event: %#v", event.Err)
	}
	event, _ = decodeConsoleEvent("response.failed", `{"response":{"error":{"error":"nested"}}}`)
	if event.Err == nil || event.Err.Message != "nested" {
		t.Fatalf("expected nested response error, got %#v", event.Err)
	}
	event, _ = decodeConsoleEvent("response.incomplete", `{"message":"top-level"}`)
	if event.Err == nil || event.Err.Message != "top-level" {
		t.Fatalf("expected top-level message, got %#v", event.Err)
	}
}

func TestDecodeConsoleEventPassesThroughUnknownType(t *testing.T) {
	event, err := decodeConsoleEvent("response.created", `{"response":{"id":"x"}}`)
	if err != nil || event.Type != "response.created" {
		t.Fatalf("unexpected passthrough event: %#v %v", event, err)
	}
}

func TestScanConsoleStreamsEventsUntilDone(t *testing.T) {
	stream := strings.Join([]string{
		"event: response.output_text.delta",
		`data: {"delta":"he"}`,
		`data: {"delta":"llo"}`,
		"data: [DONE]",
		`data: {"delta":"never"}`,
	}, "\n")

	var events []ConsoleEvent
	err := ScanConsole(bufio.NewScanner(strings.NewReader(stream)), func(e ConsoleEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 2 deltas + done, got %#v", events)
	}
	if !events[2].Done {
		t.Fatal("expected terminal done event")
	}
}

func TestScanConsolePropagatesCallbackError(t *testing.T) {
	boom := errors.New("boom")
	stream := "event: response.output_text.delta\ndata: {\"delta\":\"x\"}\n"
	err := ScanConsole(bufio.NewScanner(strings.NewReader(stream)), func(ConsoleEvent) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("expected callback error to propagate, got %v", err)
	}
}

func TestScanConsolePropagatesDecodeError(t *testing.T) {
	stream := "event: error\ndata: {\"error\":{}}\n"
	err := ScanConsole(bufio.NewScanner(strings.NewReader(stream)), func(e ConsoleEvent) error {
		if e.Err != nil {
			return e.Err
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error event to surface as scan error")
	}
}

func TestResponsesInputMessagesVariants(t *testing.T) {
	messages := ResponsesInputMessages(
		[]any{
			map[string]any{"role": "assistant", "content": "hi"},
			map[string]any{"content": "defaulted"},
			"loose text",
		},
		"be nice",
	)
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %#v", messages)
	}
	if messages[0].Role != "system" || messages[0].Content != "be nice" {
		t.Fatalf("instructions should lead as system: %#v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "hi" {
		t.Fatalf("expected assistant message intact: %#v", messages[1])
	}
	if messages[2].Role != "user" || messages[2].Content != "defaulted" {
		t.Fatalf("expected missing role to default to user: %#v", messages[2])
	}
	if messages[3].Role != "user" || messages[3].Content != "loose text" {
		t.Fatalf("expected loose string as user message: %#v", messages[3])
	}
}

func TestResponsesInputMessagesSkipsUnconvertibleValues(t *testing.T) {
	messages := ResponsesInputMessages([]any{42, nil}, "")
	if len(messages) != 1 {
		t.Fatalf("expected only nil to be skipped, got %#v", messages)
	}
	if messages[0].Role != "user" || messages[0].Content != "42" {
		t.Fatalf("expected 42 to coerce to string, got %#v", messages[0])
	}
}

func TestResponsesInputMessagesStringAndBlank(t *testing.T) {
	messages := ResponsesInputMessages("  hello  ", "   ")
	if len(messages) != 1 || messages[0].Content != "  hello  " {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	if len(ResponsesInputMessages("   ", "")) != 0 {
		t.Fatal("blank input and instructions should yield no messages")
	}
}

func TestResponsesInputMessagesIgnoresUnsupportedInput(t *testing.T) {
	if len(ResponsesInputMessages(map[string]any{"x": 1}, "sys")) != 1 {
		t.Fatal("expected only the instructions message for unsupported input")
	}
}

func TestConsoleSupportsSearch(t *testing.T) {
	if !consoleSupportsSearch("grok-build-0.1") {
		t.Fatal("grok-build-0.1 should support search")
	}
	if consoleSupportsSearch("grok-4.3-low") {
		t.Fatal("unknown model should not support search")
	}
}

func TestValueOrDefault(t *testing.T) {
	if valueOrDefault(nil, 0.7) != 0.7 {
		t.Fatal("expected fallback for nil")
	}
	if valueOrDefault(floatPtr(0.1), 0.7) != 0.1 {
		t.Fatal("expected explicit value to win")
	}
}

func TestProtocolStringValue(t *testing.T) {
	if stringValue(nil) != "" {
		t.Fatal("expected empty for nil")
	}
	if stringValue(42) != "42" {
		t.Fatal("expected numeric to stringify")
	}
	if stringValue("  padded  ") != "padded" {
		t.Fatal("expected value to be trimmed")
	}
}
