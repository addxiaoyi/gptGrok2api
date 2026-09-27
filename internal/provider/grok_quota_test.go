package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestGrokQuotaRefreshFetchesAllModesAndParsesWindows(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mode := body["modelName"]
		mu.Lock()
		seen[mode] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"remainingQueries": 4, "totalQueries": 10, "windowSizeSeconds": 3600})
	}))
	defer server.Close()

	quota := NewGrokQuota(server.URL, server.Client(), nil)
	values, err := quota.RefreshToken(context.Background(), "sso-live-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 5 {
		t.Fatalf("expected five quota modes, got %#v", values)
	}
	for _, mode := range []string{"auto", "fast", "expert", "heavy", "grok_4_3"} {
		window, ok := values[mode]
		if !ok || window.Remaining != 4 || window.Total != 10 || window.WindowSeconds != 3600 || window.ResetAt <= 0 {
			t.Fatalf("unexpected %s window: %#v", mode, window)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("expected all mode names, got %#v", seen)
	}
}

func TestGrokQuotaClassifiesInvalidCredentialMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid-credentials", http.StatusBadRequest)
	}))
	defer server.Close()

	quota := NewGrokQuota(server.URL, server.Client(), nil)
	_, err := quota.ProbeFast(context.Background(), "bad-token", nil)
	if err == nil || err != ErrGrokInvalidCredentials {
		t.Fatalf("expected invalid credential classification, got %v", err)
	}
}

func TestNewGrokQuotaDefaultsClientWhenNil(t *testing.T) {
	quota := NewGrokQuota("/rates/", nil, nil)
	if quota.Client == nil {
		t.Fatal("expected default client")
	}
	if quota.URL != "/rates" {
		t.Fatalf("URL not trimmed: %q", quota.URL)
	}
}

func TestQuotaNumberConversions(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
		ok    bool
	}{
		{"float64", float64(42), 42, true},
		{"int", 42, 42, true},
		{"json.Number", json.Number("100"), 100, true},
		{"string digits", "50", 50, true},
		{"string non-numeric", "abc", 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := quotaNumber(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("quotaNumber(%v) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMaxInt(t *testing.T) {
	if maxInt(1, 2) != 2 {
		t.Fatal("maxInt(1,2) = 2")
	}
	if maxInt(5, 3) != 5 {
		t.Fatal("maxInt(5,3) = 5")
	}
	if maxInt(-1, -2) != -1 {
		t.Fatal("maxInt(-1,-2) = -1")
	}
	if maxInt(0, 0) != 0 {
		t.Fatal("maxInt(0,0) = 0")
	}
}

func TestGrokQuotaRefreshTokenEmpty(t *testing.T) {
	quota := NewGrokQuota("http://x", nil, nil)
	_, err := quota.RefreshToken(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestGrokQuotaFetchHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	quota := NewGrokQuota(server.URL, server.Client(), nil)
	_, err := quota.ProbeFast(context.Background(), "token", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected HTTP 500 error, got %v", err)
	}
}

func TestGrokQuotaFetchMissingRemainingQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"totalQueries": 10})
	}))
	defer server.Close()

	quota := NewGrokQuota(server.URL, server.Client(), nil)
	_, err := quota.ProbeFast(context.Background(), "token", nil)
	if err == nil || !strings.Contains(err.Error(), "remainingQueries") {
		t.Fatalf("expected missing remainingQueries error, got %v", err)
	}
}

func TestGrokQuotaFetchWithCookieHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		if cookie != "custom=cookie" {
			t.Fatalf("expected custom cookie, got %q", cookie)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"remainingQueries": 4, "totalQueries": 10, "windowSizeSeconds": 3600})
	}))
	defer server.Close()

	quota := NewGrokQuota(server.URL, server.Client(), nil)
	_, err := quota.ProbeFast(context.Background(), "token", map[string]any{"cookie_header": "custom=cookie"})
	if err != nil {
		t.Fatalf("ProbeFast: %v", err)
	}
}
