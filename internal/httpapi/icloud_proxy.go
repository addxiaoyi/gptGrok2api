package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func icloudBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("ICLOUD_PRIVACY_MAIL_BASE_URL")), "/")
}

func icloudAPIKey() string {
	return strings.TrimSpace(os.Getenv("ICLOUD_PRIVACY_MAIL_API_KEY"))
}

func (s *Server) icloudBridgeStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	base := icloudBaseURL()
	if base == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":   false,
			"reachable": false,
			"error":     "ICLOUD_PRIVACY_MAIL_BASE_URL not configured",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":   true,
			"reachable": false,
			"base_url":  base,
			"error":     err.Error(),
		})
		return
	}
	if key := icloudAPIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}

	resp, err := s.requestClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":   true,
			"reachable": false,
			"base_url":  base,
			"error":     err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     true,
		"reachable":   resp.StatusCode < 500,
		"base_url":    base,
		"status_code": resp.StatusCode,
	})
}

// sidecar 把资源分成两套根：/api/icloud/* 与 /api/apple-account/* 是嵌套路由，
// 而 status/accounts/mailboxes 挂在 /api/ 根下。前端统一加了 /api/icloud 前缀，
// 所以这里按第一段做还原，避免出现 /api/icloud/icloud/session 这类双前缀。
func icloudUpstreamPath(subPath string) string {
	const nestedRoot = "icloud/"
	const appleAccountRoot = "apple-account/"

	switch {
	case strings.HasPrefix(subPath, nestedRoot):
		return "/api/" + subPath
	case strings.HasPrefix(subPath, appleAccountRoot):
		return "/api/" + subPath
	}

	head, _, _ := strings.Cut(subPath, "/")
	switch head {
	case "status", "accounts", "mailboxes":
		return "/api/" + subPath
	}

	return "/api/icloud/" + subPath
}

func (s *Server) icloudProxy(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	base := icloudBaseURL()
	if base == "" {
		writeError(w, http.StatusServiceUnavailable, "iCloud privacy mail service is not configured", "not_configured")
		return
	}

	subPath := strings.TrimPrefix(r.URL.Path, "/api/icloud/")
	var bodyReader io.Reader
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodDelete {
		var buf bytes.Buffer
		if _, copyErr := io.Copy(&buf, r.Body); copyErr == nil && buf.Len() > 0 {
			bodyReader = bytes.NewReader(buf.Bytes())
		}
	}

	targetURL := base + icloudUpstreamPath(subPath)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bodyReader)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	// 透传原始请求头（除 hop-by-hop 类 header）
	for key, values := range r.Header {
		switch strings.ToLower(key) {
		case "host", "content-length", "transfer-encoding", "connection", "te", "trailer", "upgrade", "proxy-authorization", "proxy-authenticate", "expect", "range", "if-none-match", "if-modified-since", "if-match", "if-unmodified-since", "if-range":
			continue
		}
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if key := icloudAPIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}
	// sidecar 用这个头识别主后端内部调用，从而跳过它自己的会话/全局 API Key 校验
	req.Header.Set("X-ChatGPT2API-Internal", "icloud-privacy-mail")

	resp, err := s.requestClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	// 标记代理响应，避免前端把 sidecar 401 当成主系统未授权
	w.Header().Set("X-ICloud-Privacy-Mail-Proxy", "1")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<20))
}
