package protocol

import (
	"strings"
	"testing"
)

func TestToolNamesExtractsFromFunction(t *testing.T) {
	tools := []map[string]any{
		{"function": map[string]any{"name": "search_web"}},
		{"function": map[string]any{"name": "calc_math"}},
	}
	names := ToolNames(tools)
	if len(names) != 2 || names[1] != "calc_math" {
		t.Fatalf("expected tool names, got %#v", names)
	}
}

func TestToolNamesHandlesLegacySchema(t *testing.T) {
	tools := []map[string]any{
		{"name": "legacy_tool"},
	}
	names := ToolNames(tools)
	if len(names) != 1 || names[0] != "legacy_tool" {
		t.Fatalf("expected legacy tool name, got %#v", names)
	}
}

func TestToolNamesSkipsEmpty(t *testing.T) {
	tools := []map[string]any{{}, {"function": map[string]any{"name": ""}}, {"function": nil}}
	if len(ToolNames(tools)) != 0 {
		t.Fatal("expected no names from empty tools")
	}
}

func TestToolNamesUsesFirstAvailable(t *testing.T) {
	tools := []map[string]any{
		{"function": map[string]any{"name": "func_name"}},
		{"function": map[string]any{"name": "ignored"}},
	}
	names := ToolNames(tools)
	if names[0] != "func_name" {
		t.Fatalf("expected func_name, got %#v", names)
	}
}

func TestBuildToolSystemPromptFormatsCorrectly(t *testing.T) {
	tools := []map[string]any{
		{
			"function": map[string]any{
				"name":        "search",
				"description": "search the web",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}
	prompt := BuildToolSystemPrompt(tools, "")
	if !strings.Contains(prompt, "Tool: search") {
		t.Fatalf("expected tool name in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "search the web") {
		t.Fatalf("expected description in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "WHEN TO CALL:") {
		t.Fatalf("expected WHEN TO CALL section: %s", prompt)
	}
}

func TestBuildToolSystemPromptChoiceNone(t *testing.T) {
	prompt := BuildToolSystemPrompt(nil, "none")
	if !strings.Contains(prompt, "Do NOT call any tools") {
		t.Fatalf("expected none choice: %s", prompt)
	}
}

func TestBuildToolSystemPromptChoiceRequired(t *testing.T) {
	prompt := BuildToolSystemPrompt(nil, "required")
	if !strings.Contains(prompt, "MUST output a <tool_calls>") {
		t.Fatalf("expected required choice: %s", prompt)
	}
}

func TestBuildToolSystemPromptChoiceSpecificTool(t *testing.T) {
	prompt := BuildToolSystemPrompt(nil, map[string]any{"function": map[string]any{"name": "my_tool"}})
	if !strings.Contains(prompt, "MUST call the tool named \"my_tool\"") {
		t.Fatalf("expected specific tool choice: %s", prompt)
	}
}

func TestParseToolCallsXMLStyle(t *testing.T) {
	input := `<tool_calls><tool_call><tool_name>my_tool</tool_name><parameters>{"key":"val"}</parameters></tool_call></tool_calls>`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 || calls[0].Name != "my_tool" || calls[0].Arguments != `{"key":"val"}` {
		t.Fatalf("unexpected XML tool calls: %#v", calls)
	}
	if !strings.HasPrefix(calls[0].ID, "call_") {
		t.Fatalf("expected generated call ID, got %q", calls[0].ID)
	}
}

func TestParseToolCallsXMLStyleMultipleBlocks(t *testing.T) {
	input := "<tool_calls>\n<tool_call><tool_name>first</tool_name><parameters>{\"a\":1}</parameters></tool_call>\n" +
		"<tool_call><tool_name>second</tool_name><parameters>{\"b\":2}</parameters></tool_call>\n</tool_calls>"
	calls := ParseToolCalls(input, nil)
	if len(calls) != 2 || calls[0].Name != "first" || calls[1].Name != "second" {
		t.Fatalf("expected both tool calls parsed: %#v", calls)
	}
	if calls[0].ID == calls[1].ID {
		t.Fatalf("expected unique IDs, both were %q", calls[0].ID)
	}
}

func TestParseToolCallsJSONStyle(t *testing.T) {
	input := `{"tool_calls":[{"name":"json_tool","arguments":{"id":1}}]}`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 || calls[0].Name != "json_tool" {
		t.Fatalf("unexpected JSON tool calls: %#v", calls)
	}
}

func TestParseToolCallsRequiresToolCallsKeyword(t *testing.T) {
	input := `{"calls":[{"name":"ignored"}]}`
	if ParseToolCalls(input, nil) != nil {
		t.Fatal("expected nil when 'tool_calls' keyword missing")
	}
}

func TestParseToolCallsFiltersByAllowList(t *testing.T) {
	input := "<tool_calls>\n<tool_call><tool_name>allowed</tool_name><parameters>{}</parameters></tool_call>\n" +
		"<tool_call><tool_name>blocked</tool_name><parameters>{}</parameters></tool_call>\n</tool_calls>"
	calls := ParseToolCalls(input, []string{"allowed"})
	if len(calls) != 1 || calls[0].Name != "allowed" {
		t.Fatalf("expected allow-list filter to work: %#v", calls)
	}
}

func TestParseToolCallsHandlesMissingParameters(t *testing.T) {
	input := "<tool_calls><tool_call><tool_name>no_params</tool_name></tool_call></tool_calls>"
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 {
		t.Fatalf("expected tool without params: %#v", calls)
	}
	if calls[0].Arguments != "{}" {
		t.Fatalf("expected empty args for missing params: %s", calls[0].Arguments)
	}
}

func TestParseToolCallsHandlesInvalidJSONParams(t *testing.T) {
	input := "<tool_calls><tool_call><tool_name>invalid</tool_name><parameters>not json</parameters></tool_call></tool_calls>"
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 {
		t.Fatalf("expected tool despite invalid params JSON: %#v", calls)
	}
	if calls[0].Arguments != "{}" {
		t.Fatalf("expected default empty args for invalid JSON, got %s", calls[0].Arguments)
	}
}

func TestParseToolCallsIgnoresNonMatchingName(t *testing.T) {
	input := "<tool_calls><tool_call><tool_name>some_tool</tool_name><parameters>{}</parameters></tool_call></tool_calls>"
	calls := ParseToolCalls(input, []string{"other_tool"})
	if len(calls) != 0 {
		t.Fatalf("expected no calls for non-allowed tool, got: %#v", calls)
	}
}

func TestParseToolCallsHandlesEmptyInput(t *testing.T) {
	if ParseToolCalls("", nil) != nil {
		t.Fatal("expected empty tool call input to return nil")
	}
}

func TestParseToolCallsJSONNested(t *testing.T) {
	input := `{"tool_calls":[{"name":"a","arguments":{"x":1}},{"name":"b","tool_name":"fallback","input":{"y":2}}]}`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 2 || calls[0].Name != "a" || calls[1].Name != "b" {
		t.Fatalf("expected both top-level tool_calls: %#v", calls)
	}
	if calls[1].Arguments != `{"y":2}` {
		t.Fatalf("expected input field used as arguments, got %s", calls[1].Arguments)
	}
}

func TestParseToolCallsJSONFiltersAndSkips(t *testing.T) {
	input := `{"tool_calls":[{"name":"keep","arguments":{}},{"name":"skip"},{"arguments":{}}]}`
	calls := ParseToolCalls(input, []string{"keep"})
	if len(calls) != 1 || calls[0].Name != "keep" {
		t.Fatalf("expected allow-list and empty-name filtering: %#v", calls)
	}
}

func TestParseToolCallsJSONWithoutBrace(t *testing.T) {
	if calls := ParseToolCalls("tool_calls mentioned but no JSON", nil); calls != nil {
		t.Fatalf("expected nil without JSON object, got %#v", calls)
	}
}

func TestParseToolCallsJSONMalformedBody(t *testing.T) {
	if calls := ParseToolCalls(`{"tool_calls": [`, nil); calls != nil {
		t.Fatalf("expected nil for malformed JSON, got %#v", calls)
	}
}

func TestToolStringValueExtractsCorrectly(t *testing.T) {
	if got := toolStringValue("  hello  "); got != "hello" {
		t.Fatalf("expected trimmed string, got %q", got)
	}
	if got := toolStringValue(nil); got != "" {
		t.Fatalf("expected empty for nil, got %q", got)
	}
}

func TestInjectToolPromptPreppendsSystem(t *testing.T) {
	result := InjectToolPrompt("User message", "You have tools available.")
	if !strings.HasPrefix(result, "[system]:") {
		t.Fatalf("expected system prefix: %s", result)
	}
	if !strings.Contains(result, "User message") {
		t.Fatalf("expected user message to follow: %s", result)
	}
}