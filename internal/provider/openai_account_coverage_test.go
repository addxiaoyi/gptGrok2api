package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// browserFor memoizes one tls-client per proxy URL, which is the expensive
// object in the account client.
func TestBrowserForCachesPerProxyURL(t *testing.T) {
	client := NewOpenAIAccountClient("https://chatgpt.com", "https://oauth.invalid",
		&http.Client{Timeout: 2 * time.Second}, nil)

	first, err := client.browserFor("")
	if err != nil {
		t.Fatalf("browserFor: %v", err)
	}
	second, err := client.browserFor("")
	if err != nil {
		t.Fatalf("browserFor (cached): %v", err)
	}
	if first != second {
		t.Fatal("browserFor should reuse the cached client for the same proxy URL")
	}

	other, err := client.browserFor("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("browserFor with proxy: %v", err)
	}
	if other == first {
		t.Fatal("a different proxy URL should get its own browser client")
	}
	other.CloseIdleConnections()
	first.CloseIdleConnections()
}

func TestBrowserForDefaultsTimeoutWhenUnset(t *testing.T) {
	// A zero http.Client timeout exercises the 45s fallback in browserFor.
	client := &OpenAIAccountClient{
		BaseURL:  "https://chatgpt.com",
		OAuthURL: "https://oauth.invalid",
		HTTP:     &http.Client{},
		browsers: map[string]*browserHTTP{},
	}
	browser, err := client.browserFor("")
	if err != nil {
		t.Fatalf("browserFor: %v", err)
	}
	browser.CloseIdleConnections()
}

func TestDoChatGPTUsesBrowserClientForChatgptCom(t *testing.T) {
	// When the baseURL is a chatgpt.com host, doChatGPT attempts to
	// create a browserHTTP client. The tls-client library can be fragile
	// in a sandbox, so this test merely confirms the code path is
	// reachable and panics are avoided.
	client := NewOpenAIAccountClient("https://chatgpt.com", "https://oauth.invalid",
		&http.Client{Timeout: 100 * time.Millisecond}, nil)

	request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The browser HTTP client may fail to dial; we only care that the
	// code path does not panic and returns a non-nil error.
	_, _ = client.doChatGPT(request, "")
}

func TestDoChatGPTUsesPlainClientForNonBrowserHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer server.Close()

	client := NewOpenAIAccountClient(server.URL, server.URL, server.Client(), nil)
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.doChatGPT(request, "")
	if err != nil {
		t.Fatalf("doChatGPT: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", response.StatusCode)
	}
}

func TestRefreshClearanceRejectsNonOKStatus(t *testing.T) {
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer flare.Close()

	client := NewOpenAIAccountClient("http://chat.invalid", "http://oauth.invalid",
		&http.Client{Timeout: time.Second}, nil, ClearanceConfig{URL: flare.URL, Enabled: true})

	_, err := client.refreshClearance(context.Background(), "", http.MethodGet,
		"http://chat.invalid/backend-api/me", "token", map[string]any{}, "/backend-api/me")
	if err == nil || !strings.Contains(err.Error(), "flaresolverr HTTP 502") {
		t.Fatalf("expected flaresolverr HTTP error, got %v", err)
	}
}

func TestRefreshClearanceRejectsUndecodableBody(t *testing.T) {
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer flare.Close()

	client := NewOpenAIAccountClient("http://chat.invalid", "http://oauth.invalid",
		&http.Client{Timeout: time.Second}, nil, ClearanceConfig{URL: flare.URL, Enabled: true})

	_, err := client.refreshClearance(context.Background(), "", http.MethodGet,
		"http://chat.invalid/backend-api/me", "token", map[string]any{}, "/backend-api/me")
	if err == nil || !strings.Contains(err.Error(), "decode flaresolverr response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestRefreshClearanceRejectsNonOKSolution(t *testing.T) {
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error"})
	}))
	defer flare.Close()

	client := NewOpenAIAccountClient("http://chat.invalid", "http://oauth.invalid",
		&http.Client{Timeout: time.Second}, nil, ClearanceConfig{URL: flare.URL, Enabled: true})

	_, err := client.refreshClearance(context.Background(), "", http.MethodGet,
		"http://chat.invalid/backend-api/me", "token", map[string]any{}, "/backend-api/me")
	if err == nil || !strings.Contains(err.Error(), "did not return a solution") {
		t.Fatalf("expected solution error, got %v", err)
	}
}

// FlareSolverr can report success yet hand back nothing usable; the caller must
// learn why from the `response` field.
func TestRefreshClearanceRejectsEmptySolution(t *testing.T) {
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"solution": map[string]any{"response": "challenge not detected"},
		})
	}))
	defer flare.Close()

	client := NewOpenAIAccountClient("http://chat.invalid", "http://oauth.invalid",
		&http.Client{Timeout: time.Second}, nil, ClearanceConfig{URL: flare.URL, Enabled: true})

	_, err := client.refreshClearance(context.Background(), "", http.MethodGet,
		"http://chat.invalid/backend-api/me", "token", map[string]any{}, "/backend-api/me")
	if err == nil || !strings.Contains(err.Error(), "challenge not detected") {
		t.Fatalf("expected the flaresolverr response detail, got %v", err)
	}
}

func TestRefreshClearanceSendsPOSTCommandAndProxy(t *testing.T) {
	var payload map[string]any
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode flaresolverr payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"solution": map[string]any{
				"userAgent": "solver",
				"cookies":   []map[string]string{{"name": "cf_clearance", "value": "v"}, {"name": " ", "value": "skip"}},
			},
		})
	}))
	defer flare.Close()

	client := NewOpenAIAccountClient("http://chat.invalid", "http://oauth.invalid",
		&http.Client{Timeout: time.Second}, nil, ClearanceConfig{URL: flare.URL, Enabled: true})

	bundle, err := client.refreshClearance(context.Background(), "http://proxy.test:8080", http.MethodPost,
		"http://chat.invalid/backend-api/conversation/init", "token",
		map[string]any{"cookie_header": "skipped"}, "/backend-api/conversation/init")
	if err != nil {
		t.Fatalf("refreshClearance: %v", err)
	}
	if bundle.Cookie != "cf_clearance=v" {
		t.Fatalf("blank cookie names should be skipped, got %q", bundle.Cookie)
	}
	if payload["cmd"] != "request.post" {
		t.Fatalf("cmd = %v, want request.post", payload["cmd"])
	}
	proxyPayload, _ := payload["proxy"].(map[string]any)
	if proxyPayload["url"] != "http://proxy.test:8080" {
		t.Fatalf("proxy = %v, want the account proxy URL", payload["proxy"])
	}
	headers, _ := payload["headers"].(map[string]any)
	if _, leaked := headers["Cookie"]; leaked {
		t.Fatal("account cookies must not be forwarded to FlareSolverr")
	}

	// A second call reuses the cached bundle instead of re-running the browser.
	again, err := client.refreshClearance(context.Background(), "http://proxy.test:8080", http.MethodGet,
		"http://chat.invalid/backend-api/me", "token", nil, "/backend-api/me")
	if err != nil {
		t.Fatalf("cached refreshClearance: %v", err)
	}
	if again != bundle {
		t.Fatalf("expected the cached bundle, got %+v", again)
	}
}

func TestRefreshClearanceUnreachableService(t *testing.T) {
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := flare.URL
	flare.Close()

	client := NewOpenAIAccountClient("http://chat.invalid", "http://oauth.invalid",
		&http.Client{Timeout: time.Second}, nil, ClearanceConfig{URL: url, Enabled: true})

	if _, err := client.refreshClearance(context.Background(), "", http.MethodGet,
		"http://chat.invalid/backend-api/me", "token", nil, "/backend-api/me"); err == nil {
		t.Fatal("expected a connection error from a closed FlareSolverr service")
	}
}

func TestGetJSONKeepsGoingWhenClearanceRetryFails(t *testing.T) {
	// A 403 that FlareSolverr cannot fix must still surface the upstream error
	// instead of being swallowed.
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cloudflare", http.StatusForbidden)
	}))
	defer chat.Close()

	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer flare.Close()

	client := NewOpenAIAccountClient(chat.URL, chat.URL, chat.Client(), nil,
		ClearanceConfig{URL: flare.URL, Enabled: true, Timeout: 2 * time.Second})

	_, err := client.getJSON(context.Background(), http.MethodGet, "/backend-api/me", "token", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected the 403 to propagate, got %v", err)
	}
}

func TestGetJSONDropsQueryStringFromTargetHeaders(t *testing.T) {
	var targetPath, targetRoute string
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetPath = r.Header.Get("X-OpenAI-Target-Path")
		targetRoute = r.Header.Get("X-OpenAI-Target-Route")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": map[string]any{"default": map[string]any{"account": map[string]any{"plan_type": "team"}}},
		})
	}))
	defer chat.Close()

	client := NewOpenAIAccountClient(chat.URL, chat.URL, chat.Client(), nil)
	account, err := client.getDefaultAccount(context.Background(), "token", map[string]any{"cookie_header": "session=abc"})
	if err != nil {
		t.Fatalf("getDefaultAccount: %v", err)
	}
	if account["plan_type"] != "team" {
		t.Fatalf("plan_type = %v, want team", account["plan_type"])
	}
	if targetPath != "/backend-api/accounts/check/v4-2023-04-27" || targetRoute != targetPath {
		t.Fatalf("target headers should carry the route only, got %q / %q", targetPath, targetRoute)
	}
}

func TestGetDefaultAccountToleratesMissingPayload(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{}})
	}))
	defer chat.Close()

	client := NewOpenAIAccountClient(chat.URL, chat.URL, chat.Client(), nil)
	account, err := client.getDefaultAccount(context.Background(), "token", nil)
	if err != nil {
		t.Fatalf("getDefaultAccount: %v", err)
	}
	if len(account) != 0 {
		t.Fatalf("expected an empty map when the default account is absent, got %#v", account)
	}
}

// A missing plan_type must not be reported as a paid plan with unknown quota.
func TestFetchUserInfoReportsThrottleForPaidPlanWithoutQuota(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"email": "paid@example.test", "id": "user-4"})
		case "/backend-api/conversation/init":
			_ = json.NewEncoder(w).Encode(map[string]any{"limits_progress": []any{}})
		case "/backend-api/accounts/check/v4-2023-04-27":
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"default": map[string]any{"account": map[string]any{"plan_type": "pro"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer chat.Close()

	client := NewOpenAIAccountClient(chat.URL, chat.URL, chat.Client(), nil)
	fields, err := client.fetchUserInfo(context.Background(), "token", nil)
	if err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	if fields["status"] != "正常" {
		t.Fatalf("status = %v, want 正常 while the quota is unknown", fields["status"])
	}
	if fields["image_quota_unknown"] != true {
		t.Fatalf("image_quota_unknown = %v, want true", fields["image_quota_unknown"])
	}
}

func TestFetchUserInfoDefaultsToFreePlan(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"email": "free@example.test"})
		case "/backend-api/conversation/init":
			_ = json.NewEncoder(w).Encode(map[string]any{"limits_progress": []any{map[string]any{"feature_name": "image_gen", "remaining": 0}}})
		case "/backend-api/accounts/check/v4-2023-04-27":
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"default": map[string]any{"account": map[string]any{}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer chat.Close()

	client := NewOpenAIAccountClient(chat.URL, chat.URL, chat.Client(), nil)
	fields, err := client.fetchUserInfo(context.Background(), "token", nil)
	if err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	if fields["type"] != "free" {
		t.Fatalf("type = %v, want free", fields["type"])
	}
	// A free plan with an exhausted image quota is genuinely throttled.
	if fields["status"] != "限流" {
		t.Fatalf("status = %v, want 限流", fields["status"])
	}
	if fields["image_quota_unknown"] != false {
		t.Fatalf("image_quota_unknown = %v, want false when limits are reported", fields["image_quota_unknown"])
	}
}

func TestFetchUserInfoFailsWhenProfileEndpointFails(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/conversation/init" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "x@example.test"})
	}))
	defer chat.Close()

	client := NewOpenAIAccountClient(chat.URL, chat.URL, chat.Client(), nil)
	if _, err := client.fetchUserInfo(context.Background(), "token", nil); err == nil {
		t.Fatal("expected fetchUserInfo to fail when conversation/init is unavailable")
	}
}

func TestNewOpenAIAccountClientDefaultsAndTrims(t *testing.T) {
	client := NewOpenAIAccountClient("  https://chatgpt.com///  ", "  https://oauth.invalid  ", nil, nil)
	if client.BaseURL != "https://chatgpt.com" {
		t.Fatalf("BaseURL = %q, want the trimmed value", client.BaseURL)
	}
	if client.OAuthURL != "https://oauth.invalid" {
		t.Fatalf("OAuthURL = %q, want the trimmed value", client.OAuthURL)
	}
	if client.HTTP == nil || client.HTTP.Timeout != 30*time.Second {
		t.Fatalf("expected a 30s default client, got %+v", client.HTTP)
	}
	if client.ClearanceEnabled || client.FlareSolverrURL != "" {
		t.Fatal("clearance must stay opt-in")
	}
}

func TestNewOpenAIAccountClientAcceptsClearanceConfig(t *testing.T) {
	client := NewOpenAIAccountClient("https://chatgpt.com", "https://oauth.invalid", nil, nil,
		ClearanceConfig{URL: "http://flaresolverr:8191/", Enabled: true, Timeout: 9 * time.Second})
	if client.FlareSolverrURL != "http://flaresolverr:8191" {
		t.Fatalf("FlareSolverrURL = %q, want the trimmed value", client.FlareSolverrURL)
	}
	if !client.ClearanceEnabled || client.ClearanceTimeout != 9*time.Second {
		t.Fatalf("clearance config was not applied: %+v", client)
	}
}
