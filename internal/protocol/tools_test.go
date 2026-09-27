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
	if !strings.Contains(prompt, "Do NOT call any tools") {
		t.Fatalf("expected default choice text: %s", prompt)
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
	input := `<tool_calls><tool_name>my_tool</tool_name><parameters>{"key":"val"}</parameters></tool_calls>`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 || calls[0].Name != "my_tool" || calls[0].Arguments != `{"key":"val"}` {
		t.Fatalf("unexpected XML tool calls: %#v", calls)
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
	input := `<tool_calls><tool_name>allowed</tool_name><parameters>{}</parameters><tool_name>blocked</tool_name><parameters>{}</parameters></tool_calls>`
	calls := ParseToolCalls(input, []string{"allowed"})
	if len(calls) != 1 || calls[0].Name != "allowed" {
		t.Fatalf("expected allow-list filter to work: %#v", calls)
	}
}

func TestParseToolCallsHandlesMissingParameters(t *testing.T) {
	input := `<tool_calls><tool_name>no_params</tool_name></tool_calls>`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 {
		t.Fatalf("expected tool without params: %#v", calls)
	}
	if calls[0].Arguments != "{}" {
		t.Fatalf("expected empty args for missing params: %s", calls[0].Arguments)
	}
}

func TestParseToolCallsHandlesInvalidJSONParams(t *testing.T) {
	input := `<tool_calls><tool_name>invalid</tool_name><parameters>not json</parameters></tool_calls>`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 {
		t.Fatalf("expected tool despite invalid params JSON: %#v", calls)
	}
}

func TestParseToolCallsIgnoresNonMatchingName(t *testing.T) {
	input := `<tool_calls><tool_name>some_tool</tool_name><parameters>{}</parameters></tool_calls>`
	calls := ParseToolCalls(input, []string{"other_tool"})
	if calls != nil {
		t.Fatalf("expected nil for non-allowed tool, got: %#v", calls)
	}
}

func TestParseToolCallsHandlesEmptyInput(t *testing.T) {
	if ParseToolCalls("", nil) != nil {
		t.Fatal("expected empty tool call input to return nil")
	}
}

func TestParseToolCallsJSONNested(t *testing.T) {
	input := `{"tool_calls":[{"name":"a"},{"tool_calls":[{"name":"b"}]}]`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 || calls[0].Name != "a" {
		t.Fatalf("expected only top-level tool_calls: %#v", calls)
	}
}

func TestParseToolCallsMalformedJSONFallsBackToDirect(t *testing.T) {
	input := `{"tool_calls":[{"name":"recovered"}]}`
	calls := ParseToolCalls(input, nil)
	if len(calls) != 1 {
		t.Fatalf("expected direct extraction: %#v", calls)
	}
}

func TestToolStringValueExtractsCorrectly(t *testing.T) {
	if toolStringValue("  hello  ") != "hello" {
		t.Fatal("expected trimmed string")
	}
	if toolStringValue(nil) != "" {
		t.Fatal("expected empty for nil")
	}
	if toolStringValue(map[string]any{}) != "" {
		t.Fatal("expected empty for non-string")
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