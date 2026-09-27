package model

import "testing"

func TestPublicReturnsFlatSpec(t *testing.T) {
	spec := Spec{
		ID: "test-id",
		Name: "Test Model",
		OwnedBy: "test-owner",
		Created: 1234567890,
	}
	got := spec.Public()
	if got["id"] != "test-id" || got["object"] != "model" ||
		got["created"] != int64(1234567890) || got["owned_by"] != "test-owner" || got["name"] != "Test Model" {
		t.Fatalf("unexpected public output: %#v", got)
	}
	if _, hasCap := got["capability"]; hasCap {
		t.Fatal("Public() should not include internal Capability field")
	}
}

func TestFindReturnsDisabledModel(t *testing.T) {
	disabled := Spec{ID: "disabled-model", Name: "Disabled", OwnedBy: "test", Created: 0, Enabled: false}
	enabled := Spec{ID: "enabled-model", Name: "Enabled", OwnedBy: "test", Created: 0, Enabled: true}
	if _, ok := Find([]Spec{enabled, disabled}, "enabled-model"); !ok {
		t.Fatal("expected to find enabled model")
	}
	if _, ok := Find([]Spec{enabled, disabled}, "disabled-model"); ok {
		t.Fatal("expected disabled model to be rejected by Find")
	}
}

func TestResolveChatEmptyOrWhitespace(t *testing.T) {
	for _, id := range []string{"", "   ", "\t"} {
		if _, ok := ResolveChat(id); ok {
			t.Fatalf("expected rejection for empty-like ID: %q", id)
		}
	}
}

func TestResolveChatOpenAIModels(t *testing.T) {
	for _, id := range []string{
		"auto", "gpt-5", "gpt-5-1", "gpt-5-2", "gpt-5-3",
		"gpt-5-3-mini", "gpt-5-5", "gpt-5-6", "gpt-5-6-sol",
		"gpt-5-6-terra", "gpt-5-6-luna", "gpt-5-mini",
	} {
		route, ok := ResolveChat(id)
		if !ok {
			t.Fatalf("expected find for %q", id)
		}
		if !route.OpenAI {
			t.Fatalf("expected OpenAI=true for %q", id)
		}
		if len(route.PoolCandidates) == 0 {
			t.Fatalf("expected pool candidates for %q", id)
		}
	}
}

func TestResolveChatConsoleModels(t *testing.T) {
	for _, id := range []string{
		"grok-4.3-console", "grok-4.20-0309-reasoning-console", "grok-4.20-0309-console",
		"grok-4.20-multi-agent-console", "grok-4.20-0309-non-reasoning-console",
		"grok-build-console",
	} {
		route, ok := ResolveChat(id)
		if !ok {
			t.Fatalf("expected find for %q", id)
		}
		if !route.Console {
			t.Fatalf("expected Console=true for %q", id)
		}
		if route.Mode != "console" {
			t.Fatalf("expected mode=console for %q, got %q", id, route.Mode)
		}
	}
}

func TestResolveChatGrok420Fast(t *testing.T) {
	for _, id := range []string{"grok-4.20-fast", "grok-4.3-fast"} {
		route, ok := ResolveChat(id)
		if !ok {
			t.Fatalf("expected find for %q", id)
		}
		if route.Mode != "fast" {
			t.Fatalf("expected mode=fast for %q, got %q", id, route.Mode)
		}
	}
}

func TestResolveChatGrok420Auto(t *testing.T) {
	for _, id := range []string{
		"grok-4.20-0309", "grok-4.20-0309-super",
		"grok-4.20-0309-heavy", "grok-4.20-heavy",
	} {
		route, ok := ResolveChat(id)
		if !ok {
			t.Fatalf("expected find for %q", id)
		}
		if route.Mode != "auto" && route.Mode != "heavy" {
			t.Fatalf("expected mode=auto or heavy for %q, got %q", id, route.Mode)
		}
	}
}

func TestResolveChatGrok420Expert(t *testing.T) {
	for _, id := range []string{
		"grok-4.20-0309-reasoning", "grok-4.20-expert",
		"grok-4.20-0309-reasoning-super", "grok-4.20-0309-reasoning-heavy",
	} {
		route, ok := ResolveChat(id)
		if !ok {
			t.Fatalf("expected find for %q", id)
		}
		if route.Mode != "expert" {
			t.Fatalf("expected mode=expert for %q, got %q", id, route.Mode)
		}
	}
}

func TestResolveChatNonReasoningSpecificPool(t *testing.T) {
	cases := map[string][]string{
		"grok-4.20-0309-non-reasoning-super": {"super", "heavy"},
		"grok-4.20-0309-non-reasoning-heavy": {"heavy"},
	}
	for id, expected := range cases {
		route, ok := ResolveChat(id)
		if !ok {
			t.Fatalf("expected find for %q", id)
		}
		if route.Mode != "fast" {
			t.Fatalf("expected mode=fast for %q, got %q", id, route.Mode)
		}
		if route.PoolCandidates[0] != expected[0] {
			t.Fatalf("expected pool start with %v for %q, got %v", expected, id, route.PoolCandidates)
		}
	}
}

func TestResolveChatBetaModelCustomRoute(t *testing.T) {
	route, ok := ResolveChat("grok-4.3-beta")
	if !ok {
		t.Fatal("expected find for grok-4.3-beta")
	}
	if route.Mode != "grok-420-computer-use-sa" {
		t.Fatalf("expected custom mode for beta, got %q", route.Mode)
	}
}

func TestResolveChatUnknownReturnsFalse(t *testing.T) {
	for _, id := range []string{
		"unknown-model", "gpt-4", "gpt-4o", "model-0",
		"grok-0", "grok-unknown",
	} {
		if _, ok := ResolveChat(id); ok {
			t.Fatalf("expected no match for unknown: %q", id)
		}
	}
}

func TestIsOpenAIChatModelTrimsWhitespace(t *testing.T) {
	for _, id := range []string{"  gpt-5  ", "\tgpt-5-mini\n"} {
		if !isOpenAIChatModel(id) {
			t.Fatalf("expected isOpenAIChatModel to handle whitespace for %q", id)
		}
	}
}

func TestIsOpenAIChatModelRejectsDisabled(t *testing.T) {
	for _, id := range []string{"not-a-model", "gpt-image-2", "gpt5"} {
		if isOpenAIChatModel(id) {
			t.Fatalf("expected rejection for %q", id)
		}
	}
}