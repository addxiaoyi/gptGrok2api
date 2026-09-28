package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestXAIProbeRefreshesAfterUnauthorizedAndReadsBilling(t *testing.T) {
	responseCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			responseCalls++
			if r.Header.Get("Authorization") == "Bearer old-token" {
				http.Error(w, `{"error":{"code":"invalid_token","message":"expired"}}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp-1", "usage": map[string]any{"total_tokens": 1}})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"creditUsagePercent": 23.5})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "new-token", "refresh_token": "new-refresh", "id_token": "new-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL+"/token", server.Client()).Probe(context.Background(), map[string]any{"access_token": "old-token", "refresh_token": "refresh"})
	if result.Status != "valid" || result.AccessToken != "new-token" || result.RefreshToken != "new-refresh" || responseCalls != 2 {
		t.Fatalf("unexpected probe result: %#v calls=%d", result, responseCalls)
	}
	if result.Quota["used_percent"] != 23.5 || result.Quota["remaining_percent"] != 76.5 {
		t.Fatalf("unexpected quota: %#v", result.Quota)
	}
}

func TestXAIProbeValidWithoutRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1"})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"creditUsagePercent": 10})
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Status != "valid" || result.AccessToken != "tok" {
		t.Fatalf("probe result = %#v", result)
	}
	if used, ok := result.Quota["used_percent"].(float64); !ok || used != 10.0 {
		t.Fatalf("quota = %#v", result.Quota)
	}
}

func TestXAIProbeLimitedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "rate_limited"}})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"creditUsagePercent": 55})
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Status != "limited" || result.Code != "rate_limited" {
		t.Fatalf("probe result = %#v", result)
	}
	if result.Quota == nil {
		t.Fatal("expected quota on limited")
	}
}

func TestXAIProbeInvalidNoRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"bad_auth"}}`, http.StatusForbidden)
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Status != "invalid" || result.Code != "bad_auth" {
		t.Fatalf("probe result = %#v", result)
	}
}

func TestXAIProbeUnknownStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "oops", http.StatusInternalServerError)
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Status != "unknown" {
		t.Fatalf("probe result = %#v", result)
	}
}

func TestXAIProbeMissingAccessToken(t *testing.T) {
	probe := NewXAIProbe("http://x", "http://y", nil)
	result := probe.Probe(context.Background(), map[string]any{})
	if result.Code != "invalid_credentials" {
		t.Fatalf("expected invalid_credentials, got %s", result.Code)
	}
}

func TestXAIProbeTestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"output": "OK"})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"creditUsagePercent": 30})
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Test(context.Background(), map[string]any{"access_token": "tok"}, "custom-model", "say hi")
	if result.Status != "valid" {
		t.Fatalf("test result = %#v", result)
	}
}

func TestXAIProbeTestWithExpiredJWTRefresh(t *testing.T) {
	exp := time.Now().Add(30 * time.Second).Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	expiredToken := header + "." + payload + "." + sig

	var refreshed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			refreshed = true
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "new-access", "refresh_token": "new-refresh", "id_token": "new-id"})
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"creditUsagePercent": 1})
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL+"/token", server.Client()).Test(context.Background(), map[string]any{
		"access_token": expiredToken,
		"refresh_token": "old-refresh",
	}, "", "")
	if !refreshed {
		t.Fatal("expected refresh call for expired token")
	}
	if result.Status != "valid" || result.AccessToken != "new-access" {
		t.Fatalf("test result = %#v", result)
	}
}

func TestXAIProbeRefreshFailureKeepsOldToken(t *testing.T) {
	exp := time.Now().Add(30 * time.Second).Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	expiredToken := header + "." + payload + "." + sig

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			http.Error(w, `{"error_description":"refresh dead"}`, http.StatusBadRequest)
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"creditUsagePercent": 1})
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL+"/token", server.Client()).Probe(context.Background(), map[string]any{
		"access_token": expiredToken,
		"refresh_token": "dead-refresh",
	})
	if result.AccessToken != expiredToken {
		t.Fatalf("expected old token kept on refresh failure, got %s", result.AccessToken)
	}
	if result.Status != "valid" {
		t.Fatalf("probe result = %#v", result)
	}
}

func TestXAIProbeHTTPTransportError(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed.Close()

	probe := NewXAIProbe(closed.URL, closed.URL, &http.Client{Timeout: time.Second})
	result := probe.Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Status != "unknown" || result.Error == "" {
		t.Fatalf("expected transport error result, got %#v", result)
	}
}

func TestXAIProbeBillingFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/billing":
			http.Error(w, "billing broken", http.StatusBadGateway)
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Quota != nil {
		t.Fatalf("expected nil quota on billing failure, got %#v", result.Quota)
	}
}

func TestXAIProbeBillingNoCreditField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/billing":
			_ = json.NewEncoder(w).Encode(map[string]any{"other": 1})
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Quota != nil {
		t.Fatalf("expected nil quota without credit field, got %#v", result.Quota)
	}
}

func TestXAIProbeBillingInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/billing":
			_, _ = w.Write([]byte("not json"))
		}
	}))
	defer server.Close()

	result := NewXAIProbe(server.URL, server.URL, server.Client()).Probe(context.Background(), map[string]any{"access_token": "tok"})
	if result.Quota != nil {
		t.Fatalf("expected nil quota on bad json, got %#v", result.Quota)
	}
}

func TestXAIProbeRefreshErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error_description":"bad refresh"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	probe := NewXAIProbe("http://localhost:1", server.URL, &http.Client{})
	_, err := probe.refresh(context.Background(), "tok")
	if err == nil || !strings.Contains(err.Error(), "bad refresh") {
		t.Fatalf("expected bad refresh error, got %v", err)
	}
}

func TestTokenExpired(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	future := time.Now().Add(time.Hour).Unix()
	futurePayload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(future, 10) + `}`))
	past := time.Now().Add(-time.Hour).Unix()
	pastPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(past, 10) + `}`))

	cases := []struct {
		token string
		want  bool
	}{
		{header + "." + futurePayload + ".s", false},
		{header + "." + pastPayload + ".s", true},
		{"garbage", false},
		{header + "." + base64.RawURLEncoding.EncodeToString([]byte("notjson")) + ".s", false},
		{"a.b", false},
	}
	for _, c := range cases {
		if got := tokenExpired(c.token); got != c.want {
			t.Fatalf("tokenExpired(%q)=%v want %v", c.token, got, c.want)
		}
	}
}

func TestNumberValue(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(1.5), 1.5, true},
		{int(2), 2, true},
		{int64(3), 3, true},
		{json.Number("4"), 4, true},
		{"notnum", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := numberValue(c.in)
		if ok != c.ok || got != c.want {
			t.Fatalf("numberValue(%v)=(%v,%v) want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestXAIFirstNonEmpty(t *testing.T) {
	if got := xaiFirstNonEmpty("", "  ", "value", "later"); got != "value" {
		t.Fatalf("first non empty = %q", got)
	}
	if got := xaiFirstNonEmpty("", "   "); got != "" {
		t.Fatalf("all blank = %q", got)
	}
}

func TestXAIErrorNested(t *testing.T) {
	code, msg := xaiError([]byte(`{"error":{"code":"e1","message":"m1"}}`))
	if code != "e1" || msg != "m1" {
		t.Fatalf("nested = %q, %q", code, msg)
	}
	code, msg = xaiError([]byte(`{"code":"e2","error":"m2"}`))
	if code != "e2" || msg != "m2" {
		t.Fatalf("flat = %q, %q", code, msg)
	}
	code, msg = xaiError([]byte(`{"message":"m3"}`))
	if code != "" || msg != "m3" {
		t.Fatalf("message only = %q, %q", code, msg)
	}
	code, msg = xaiError([]byte(`raw-text`))
	if code != "" || msg != "raw-text" {
		t.Fatalf("raw = %q, %q", code, msg)
	}
}
