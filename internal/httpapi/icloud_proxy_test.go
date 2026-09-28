package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/auucoder/gptgrok2api-go/internal/auth"
	"github.com/auucoder/gptgrok2api-go/internal/config"
	"github.com/auucoder/gptgrok2api-go/internal/store"
)

func TestICloudBridgeStatusNotConfigured(t *testing.T) {
	os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")
	server := newServerWithAdminKey(t, "test-admin-key")

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/icloud/bridge-status", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key")
	server.icloudBridgeStatus(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if enabled, _ := body["enabled"].(bool); enabled {
		t.Fatalf("expected enabled=false, got %#v", body)
	}
	if reachable, _ := body["reachable"].(bool); reachable {
		t.Fatalf("expected reachable=false, got %#v", body)
	}
}

func TestICloudBridgeStatusReachable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	os.Setenv("ICLOUD_PRIVACY_MAIL_BASE_URL", upstream.URL)
	defer os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")

	server := newServerWithAdminKey(t, "test-admin-key")

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/icloud/bridge-status", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key")
	server.icloudBridgeStatus(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if reachable, _ := body["reachable"].(bool); !reachable {
		t.Fatalf("expected reachable=true, got %#v", body)
	}
	if got, _ := body["status_code"].(float64); got != 200 {
		t.Fatalf("expected status_code 200, got %#v", body)
	}
}

func TestICloudProxyPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ICloud-Probe") != "1" {
			t.Errorf("upstream lost request header: %#v", r.Header)
		}
		w.Header().Set("X-ICloud-Upstream", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	os.Setenv("ICLOUD_PRIVACY_MAIL_BASE_URL", upstream.URL)
	defer os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")

	server := newServerWithAdminKey(t, "test-admin-key")
	server.requestClient = upstream.Client()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/icloud/health?probe=1", strings.NewReader(`{"x":1}`))
	req.Header.Set("Authorization", "Bearer test-admin-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ICloud-Probe", "1")
	server.icloudProxy(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-ICloud-Upstream"); got != "yes" {
		t.Fatalf("expected upstream header to be copied, got %q", got)
	}
	if got := recorder.Header().Get("X-ICloud-Privacy-Mail-Proxy"); got != "1" {
		t.Fatalf("expected proxy marker header, got %q", got)
	}
	if strings.Contains(recorder.Body.String(), "admin endpoint not found") {
		t.Fatalf("proxy fell through to adminAPI: %s", recorder.Body.String())
	}
}

func TestICloudProxyServiceUnavailable(t *testing.T) {
	os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")
	server := newServerWithAdminKey(t, "test-admin-key")

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/icloud/status", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key")
	server.icloudProxy(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestICloudProxyNotAuthorized(t *testing.T) {
	os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")
	server := newServerWithAdminKey(t, "test-admin-key")

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/icloud/status", nil)
	// No auth header
	server.icloudProxy(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestICloudUpstreamPath(t *testing.T) {
	cases := []struct {
		gatewayPath string
		upstream    string
	}{
		{"/api/icloud/status", "/api/status"},
		{"/api/icloud/session", "/api/icloud/session"},
		{"/api/icloud/accounts", "/api/accounts"},
		{"/api/icloud/mailboxes", "/api/mailboxes"},
		{"/api/icloud/mailboxes/mb-1/messages", "/api/mailboxes/mb-1/messages"},
		{"/api/icloud/mailboxes/mb-1/code", "/api/mailboxes/mb-1/code"},
		{"/api/icloud/icloud/session/check", "/api/icloud/session/check"},
		{"/api/icloud/icloud/protocol-login/start", "/api/icloud/protocol-login/start"},
		{"/api/icloud/icloud/scheduler/status", "/api/icloud/scheduler/status"},
		{"/api/icloud/apple-account/login/start", "/api/apple-account/login/start"},
	}

	for _, tc := range cases {
		subPath := strings.TrimPrefix(tc.gatewayPath, "/api/icloud/")
		if got := icloudUpstreamPath(subPath); got != tc.upstream {
			t.Errorf("%s -> %s, want %s", tc.gatewayPath, got, tc.upstream)
		}
	}
}

func TestICloudProxyHitsRealUpstreamPath(t *testing.T) {
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	os.Setenv("ICLOUD_PRIVACY_MAIL_BASE_URL", upstream.URL)
	defer os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")

	server := newServerWithAdminKey(t, "test-admin-key")
	server.requestClient = upstream.Client()

	for _, gatewayPath := range []string{"/api/icloud/status", "/api/icloud/icloud/session/check", "/api/icloud/accounts"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, gatewayPath, nil)
		req.Header.Set("Authorization", "Bearer test-admin-key")
		server.icloudProxy(recorder, req)

		want := icloudUpstreamPath(strings.TrimPrefix(gatewayPath, "/api/icloud/"))
		if seenPath != want {
			t.Errorf("%s forwarded to %s, want %s", gatewayPath, seenPath, want)
		}
	}
}

func newServerWithAdminKey(t *testing.T, adminKey string) *Server {
	t.Helper()
	repo := store.New(t.TempDir()+"/accounts.json", t.TempDir()+"/auth_keys.json", t.TempDir()+"/config.json")
	authValidator := auth.New(adminKey, adminKey, "", false, repo)
	return &Server{
		cfg:           config.Config{AdminKey: adminKey},
		auth:          authValidator,
		requestClient: &http.Client{},
	}
}

func TestICloudBridgeStatusFallsBackOnConnectionError(t *testing.T) {
	os.Setenv("ICLOUD_PRIVACY_MAIL_BASE_URL", "http://127.0.0.1:1")
	defer os.Unsetenv("ICLOUD_PRIVACY_MAIL_BASE_URL")

	server := newServerWithAdminKey(t, "test-admin-key")

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/icloud/bridge-status", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key")
	server.icloudBridgeStatus(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if reachable, _ := body["reachable"].(bool); reachable {
		t.Fatalf("expected reachable=false on connection error, got %#v", body)
	}
}
