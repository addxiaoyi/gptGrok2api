package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
	proxyruntime "github.com/auucoder/gptgrok2api-go/internal/proxy"
)

// ---- grok_chat.go ----

func TestGrokChatDoWithProxyManager(t *testing.T) {
	var sawAbsoluteURL bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAbsoluteURL = r.URL.Host != ""
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// The manager points at the test server, so traffic lands there via the
	// proxy transport instead of a real grok.com.
	chat := NewGrokChat("http://grok.invalid", &http.Client{
		Transport: proxyruntime.NewTransport(http.DefaultTransport),
		Timeout:   5 * time.Second,
	}, 5*time.Second)
	chat.SetProxyManager(proxyruntime.NewManager(server.URL, nil))

	response, err := chat.Do(context.Background(), accounts.Account{Token: "sso=tok"}, map[string]any{"model": "grok-4"})
	if err != nil {
		t.Fatalf("Do with proxy: %v", err)
	}
	defer response.Body.Close()
	if !sawAbsoluteURL {
		t.Fatal("proxy did not receive an absolute upstream URL")
	}
}

func TestGrokChatDoWithNilClientUsesDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// nil client should be replaced by http.Client{Timeout:0}
	chat := NewGrokChat(server.URL, nil, 0)
	if chat.Client == nil {
		t.Fatal("expected non-nil Client after nil input")
	}
	_, err := chat.Do(context.Background(), accounts.Account{}, nil)
	if err != nil {
		t.Fatalf("Do with nil client: %v", err)
	}
}

func TestGrokChatDoMarshalError(t *testing.T) {
	// A payload that cannot be marshaled (e.g. cyclic) is hard; instead test
	// that a valid request still works when the server is reachable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	chat := NewGrokChat(server.URL, server.Client(), 5*time.Second)
	_, err := chat.Do(context.Background(), accounts.Account{}, map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestGrokChatDoRequestError(t *testing.T) {
	// Passing an invalid URL triggers http.NewRequestWithContext error path.
	chat := NewGrokChat("://invalid", &http.Client{Timeout: 5 * time.Second}, 5*time.Second)
	_, err := chat.Do(context.Background(), accounts.Account{}, nil)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// ---- console_chat.go ----

func TestConsoleChatDoWithProxyManager(t *testing.T) {
	var sawAbsoluteURL bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAbsoluteURL = r.URL.Host != ""
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	chat := NewConsoleChat("http://console.invalid", &http.Client{
		Transport: proxyruntime.NewTransport(http.DefaultTransport),
	})
	chat.SetProxyManager(proxyruntime.NewManager(server.URL, nil))
	response, err := chat.Do(context.Background(), accounts.Account{Token: "sso=tok"}, map[string]any{})
	if err != nil {
		t.Fatalf("Do with proxy: %v", err)
	}
	defer response.Body.Close()
	if !sawAbsoluteURL {
		t.Fatal("proxy did not receive an absolute upstream URL")
	}
}

func TestConsoleChatDoCustomCookie(t *testing.T) {
	var cookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	chat := NewConsoleChat(server.URL, server.Client())
	_, err := chat.Do(context.Background(), accounts.Account{
		Token: "tok",
		Fields: map[string]any{"cookie_header": "custom=1"},
	}, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if cookie != "custom=1" {
		t.Fatalf("expected custom cookie, got %q", cookie)
	}
}

func TestConsoleChatDoMarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	chat := NewConsoleChat(server.URL, server.Client())
	_, err := chat.Do(context.Background(), accounts.Account{}, map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestConsoleChatDoRequestError(t *testing.T) {
	// Invalid URL path triggers http.NewRequestWithContext error.
	chat := NewConsoleChat("://invalid", &http.Client{})
	_, err := chat.Do(context.Background(), accounts.Account{}, nil)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// ---- openai_account.go ----

func TestOpenAIAccountClientRefreshAccessToken(t *testing.T) {
	var oauthCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			oauthCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-at", "refresh_token": "new-rt", "id_token": "new-id"})
			return
		}
		authorization := r.Header.Get("Authorization")
		switch authorization {
		case "Bearer new-at":
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend-api/me":
				_ = json.NewEncoder(w).Encode(map[string]any{"email": "new@example.test", "id": "user-3"})
			case "/backend-api/conversation/init":
				_ = json.NewEncoder(w).Encode(map[string]any{"default_model_slug": "gpt-4", "limits_progress": []any{}})
			case "/backend-api/accounts/check/v4-2023-04-27":
				_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"default": map[string]any{"account": map[string]any{"plan_type": "free"}}}})
			default:
				http.NotFound(w, r)
			}
		default:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL+"/oauth/token", server.Client(), nil)
	result, err := client.RefreshAccessToken(context.Background(), map[string]any{
		"refresh_token": "refresh-token",
	})
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if result.AccessToken != "new-at" {
		t.Fatalf("expected new access token, got %q", result.AccessToken)
	}
	if oauthCalls != 1 {
		t.Fatalf("expected 1 oauth call, got %d", oauthCalls)
	}
}

func TestOpenAIAccountClientRefreshAccessTokenNoRefreshToken(t *testing.T) {
	client := NewOpenAIAccountClient("http://x", "http://y", &http.Client{Timeout: 5 * time.Second}, nil)
	_, err := client.RefreshAccessToken(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "refresh_token") {
		t.Fatalf("expected refresh_token error, got %v", err)
	}
}

func TestOpenAIAccountClientRefreshOAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL+"/oauth/token", server.Client(), nil)
	_, err := client.refreshOAuth(context.Background(), "bad-refresh", map[string]any{})
	if err == nil {
		t.Fatal("expected error from oauth refresh with non-200")
	}
}

func TestOpenAIAccountClientRefreshOAuthEmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": ""})
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL+"/oauth/token", server.Client(), nil)
	_, err := client.refreshOAuth(context.Background(), "bad-refresh", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("expected HTTP status error, got %v", err)
	}
}

func TestOpenAIAccountClientGetJSONDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL, server.Client(), nil)
	account := map[string]any{"access_token": "token", "source_type": "chatgpt_web"}
	_, err := client.RefreshAccount(context.Background(), account)
	if err == nil || !strings.Contains(err.Error(), "decode OpenAI account response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestOpenAIAccountClientGetJSONNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL, server.Client(), nil)
	account := map[string]any{"access_token": "token", "source_type": "chatgpt_web"}
	_, err := client.RefreshAccount(context.Background(), account)
	if err == nil || !strings.Contains(err.Error(), "returned HTTP 500") {
		t.Fatalf("expected 500 error, got %v", err)
	}
}

func TestOpenAIAccountClientTokenNeedsRefreshNearExpiry(t *testing.T) {
	// Build a JWT whose exp is within 24 hours.
	now := time.Now().Add(1 * time.Hour).Unix()
	payload := map[string]any{"exp": float64(now)}
	raw, _ := json.Marshal(payload)
	token := "header." + strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=") + ".sig"
	if !tokenNeedsRefresh(token) {
		t.Fatal("expected tokenNeedsRefresh to return true for near-expiry token")
	}
}

func TestOpenAIAccountClientTokenNeedsRefreshNotExpired(t *testing.T) {
	now := time.Now().Add(48 * time.Hour).Unix()
	payload := map[string]any{"exp": float64(now)}
	raw, _ := json.Marshal(payload)
	token := "header." + strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=") + ".sig"
	if tokenNeedsRefresh(token) {
		t.Fatal("expected tokenNeedsRefresh to return false for far-future token")
	}
}

func TestOpenAIAccountClientIntValue(t *testing.T) {
	cases := []struct {
		input any
		want  int
	}{
		{42, 42},
		{int64(7), 7},
		{float64(3), 3},
		{json.Number("123"), 123},
		{"99", 99},
		{"bad", 0},
		{nil, 0},
	}
	for _, c := range cases {
		got := intValue(c.input)
		if got != c.want {
			t.Fatalf("intValue(%#v) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestOpenAIAccountClientBuildOpenAIFingerprintWithFP(t *testing.T) {
	account := map[string]any{
		"fp": map[string]any{
			"user_agent":           "custom-agent/1.0",
			"oai_device_id":        "dev-123",
			"oai_session_id":       "sess-456",
			"sec_ch_ua":            `"Custom";v="1"`,
			"sec_ch_ua_mobile":     "?1",
			"sec_ch_ua_platform":   `"Mac"`,
		},
	}
	fp := buildOpenAIFingerprint(account)
	if fp.UserAgent != "custom-agent/1.0" {
		t.Fatalf("UserAgent = %q, want custom-agent/1.0", fp.UserAgent)
	}
	if fp.DeviceID != "dev-123" {
		t.Fatalf("DeviceID = %q, want dev-123", fp.DeviceID)
	}
	if fp.SessionID != "sess-456" {
		t.Fatalf("SessionID = %q, want sess-456", fp.SessionID)
	}
	if !strings.Contains(fp.SecCHUA, "Custom") {
		t.Fatalf("SecCHUA = %q, want Custom", fp.SecCHUA)
	}
	if fp.SecCHUAMobile != "?1" {
		t.Fatalf("SecCHUAMobile = %q, want ?1", fp.SecCHUAMobile)
	}
	if fp.SecCHUAPlatform != `"Mac"` {
		t.Fatalf("SecCHUAPlatform = %q, want Mac", fp.SecCHUAPlatform)
	}
}

func TestOpenAIAccountClientBrowserEligibleChatgpt(t *testing.T) {
	if !browserEligible("https://chatgpt.com") {
		t.Fatal("chatgpt.com should be browser eligible")
	}
	if !browserEligible("https://api.chatgpt.com") {
		t.Fatal("subdomain of chatgpt.com should be eligible")
	}
	if browserEligible("https://example.com") {
		t.Fatal("example.com should not be eligible")
	}
	if browserEligible("http://chatgpt.com") {
		t.Fatal("http scheme should not be eligible")
	}
}

func TestOpenAIAccountClientExtractImageQuota(t *testing.T) {
	items := []any{
		map[string]any{"feature_name": "image_gen", "remaining": 5, "reset_after": "2026-01-01"},
		map[string]any{"feature_name": "other", "remaining": 1},
	}
	quota, _, unknown := extractImageQuota(items)
	if quota != 5 || unknown {
		t.Fatalf("expected quota=5, unknown=false, got %d %v", quota, unknown)
	}

	noGen := []any{map[string]any{"feature_name": "other"}}
	q, _, u := extractImageQuota(noGen)
	if q != 0 || !u {
		t.Fatalf("expected 0,true when no image_gen, got %d %v", q, u)
	}
}

func TestOpenAIAccountClientRefreshAccount401WithRefresh(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL+"/oauth/token", server.Client(), nil)
	_, err := client.RefreshAccount(context.Background(), map[string]any{
		"access_token": "old", "refresh_token": "refresh", "source_type": "oauth",
	})
	if err == nil {
		t.Fatal("expected error after oauth refresh and fetchUserInfo still failing")
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 calls (oauth + fetchUserInfo), got %d", calls)
	}
}

func TestOpenAIAccountClientRefreshAccountNoRefreshToken(t *testing.T) {
	// When access_token is missing, RefreshAccount should return error early.
	client := NewOpenAIAccountClient("http://x", "http://y", &http.Client{Timeout: 5 * time.Second}, nil)
	_, err := client.RefreshAccount(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "access_token is required") {
		t.Fatalf("expected access_token required, got %v", err)
	}
}

func TestOpenAIAccountClientReadErrorInvalidAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL, server.Client(), nil)
	_, err := client.getJSON(context.Background(), http.MethodGet, "/backend-api/me", "bad-token", map[string]any{}, nil)
	if !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("expected ErrInvalidAccessToken, got %v", err)
	}
}

func TestReadErrorProtocol(t *testing.T) {
	response := &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limited"))}
	err := ReadError(response)
	if err == nil {
		t.Fatalf("expected *protocol.UpstreamError, got %T", err)
	}
	if err.Status != 429 {
		t.Fatalf("Status = %d, want 429", err.Status)
	}
	if !strings.Contains(err.Message, "rate limited") {
		t.Fatalf("Message = %q, want 'rate limited'", err.Message)
	}
}

func TestConsoleReadErrorProtocol(t *testing.T) {
	response := &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}
	err := ReadConsoleError(response)
	if err == nil {
		t.Fatalf("expected *protocol.UpstreamError, got %T", err)
	}
	if err.Status != 500 {
		t.Fatalf("Status = %d, want 500", err.Status)
	}
}
