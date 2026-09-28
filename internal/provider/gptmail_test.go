package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestGPTMailPublicStatusAndKeyRefreshRedactKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/public-key-status" || r.Header.Get("X-Public-Key-Reveal") != "click" || r.URL.Query().Get("reveal") != "1" {
			t.Fatalf("unexpected public status request: %s headers=%v", r.URL.String(), r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"is_active": true, "daily_limit": 100, "used_today": 12, "remaining_today": 88, "key": "public-secret-key"}})
	}))
	defer server.Close()

	client := NewGPTMail(server.Client())
	config := map[string]any{"api_base": server.URL, "key_mode": "public", "default_domain": "example.test"}
	result, err := client.Status(context.Background(), config, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["remaining_today"] != float64(88) || result["key_hint"] != "publi...-key" || result["api_key"] != nil {
		t.Fatalf("unexpected redacted status: %#v", result)
	}
	_, err = client.Status(context.Background(), config, false)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected cached status, got %d upstream calls", calls)
	}
	refreshed, err := client.RefreshPublicKey(context.Background(), config, true)
	if err != nil || refreshed["key_hint"] != "publi...-key" {
		t.Fatalf("unexpected refreshed status: %#v %v", refreshed, err)
	}
}

func TestGPTMailCustomStatusUsesAPIKeyAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stats" || r.Header.Get("X-API-Key") != "custom-secret" {
			t.Fatalf("unexpected custom request: %s key=%q", r.URL.Path, r.Header.Get("X-API-Key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "usage": map[string]any{"daily_limit": 50, "used_today": 7, "remaining_today": 43, "total_limit": 500}})
	}))
	defer server.Close()
	result, err := NewGPTMail(server.Client()).Status(context.Background(), map[string]any{"api_base": server.URL, "api_key": "custom-secret"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result["source"] != "stats" || result["remaining_today"] != float64(43) || result["key_hint"] != "custo...cret" {
		t.Fatalf("unexpected custom result: %#v", result)
	}
}

func TestNewGPTMailNilClient(t *testing.T) {
	client := NewGPTMail(nil)
	if client.HTTP == nil {
		t.Fatal("expected default HTTP client")
	}
	if client.HTTP.Timeout != 30*time.Second {
		t.Fatalf("expected 30s timeout, got %s", client.HTTP.Timeout)
	}
	if client.cache == nil {
		t.Fatal("expected initialized cache map")
	}
}

func TestNewGPTMailWithClient(t *testing.T) {
	hc := &http.Client{Timeout: 5 * time.Second}
	client := NewGPTMail(hc)
	if client.HTTP != hc {
		t.Fatal("expected the provided client")
	}
}

func TestGPTMailMaskKey(t *testing.T) {
	if got := gptMailMaskKey(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := gptMailMaskKey("abc"); got != "***" {
		t.Fatalf("short: %q", got)
	}
	if got := gptMailMaskKey("12345678"); got != "********" {
		t.Fatalf("len8: %q", got)
	}
	if got := gptMailMaskKey("abcdefghij"); got != "abcde...ghij" {
		t.Fatalf("len10: %q", got)
	}
}

func TestGPTMailRefreshPublicKeyRejectsCustomMode(t *testing.T) {
	client := NewGPTMail(nil)
	_, err := client.RefreshPublicKey(context.Background(), map[string]any{"key_mode": "custom", "api_key": "k"}, false)
	if err == nil {
		t.Fatal("expected error for custom mode refresh")
	}
}

func TestGPTMailRefreshPublicKeyMissingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"is_active": true}})
	}))
	defer server.Close()
	client := NewGPTMail(server.Client())
	_, err := client.RefreshPublicKey(context.Background(), map[string]any{"api_base": server.URL, "key_mode": "public"}, true)
	if err == nil {
		t.Fatal("expected error when public key is empty")
	}
}

func TestMapValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  map[string]any
	}{
		{"nil input", nil, map[string]any{}},
		{"empty map", map[string]any{}, map[string]any{}},
		{"valid map", map[string]any{"key": "value"}, map[string]any{"key": "value"}},
		{"nested map", map[string]any{"outer": map[string]any{"inner": 1}}, map[string]any{"outer": map[string]any{"inner": 1}}},
		{"string input", "not a map", map[string]any{}},
		{"int input", 42, map[string]any{}},
		{"slice input", []any{1, 2, 3}, map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapValue(tt.input)
			if got == nil {
				t.Fatal("mapValue returned nil")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.want))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
