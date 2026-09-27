package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
	"github.com/auucoder/gptgrok2api-go/internal/protocol"
	proxyruntime "github.com/auucoder/gptgrok2api-go/internal/proxy"
)

func newStubOpenAIImageServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func writeJSON(w http.ResponseWriter, value any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(value)
}

func readJSON(r *http.Request, value *map[string]any) error {
	return json.NewDecoder(r.Body).Decode(value)
}

func TestWithOpenAIImageStageAndEgressAttachObservers(t *testing.T) {
	ctx := context.Background()
	if got := WithOpenAIImageStage(ctx, nil); got != ctx {
		t.Fatal("nil stage observer must return the original context")
	}
	if got := WithOpenAIImageEgress(ctx, nil); got != ctx {
		t.Fatal("nil egress observer must return the original context")
	}

	observed := map[string]time.Duration{}
	ctx = WithOpenAIImageStage(ctx, func(metric string, elapsed time.Duration) {
		observed[metric] = elapsed
	})
	var egress []string
	ctx = WithOpenAIImageEgress(ctx, func(proxyURL string) {
		egress = append(egress, proxyURL)
	})

	notifyOpenAIImageStage(ctx, "bootstrap_ms", time.Now().Add(-5*time.Millisecond))
	notifyOpenAIImageEgress(ctx, "http://proxy.invalid:8080")

	if elapsed, ok := observed["bootstrap_ms"]; !ok || elapsed <= 0 {
		t.Fatalf("stage observer did not receive a positive duration: %#v", observed)
	}
	if len(egress) != 1 || egress[0] != "http://proxy.invalid:8080" {
		t.Fatalf("unexpected egress observations: %#v", egress)
	}
}

func TestSetProxyManagerSwapsEgressSource(t *testing.T) {
	imageClient := NewOpenAIImage("https://example.invalid", nil, nil, time.Second)
	if imageClient.Proxy != nil {
		t.Fatal("expected a nil proxy manager before SetProxyManager")
	}
	imageClient.SetProxyManager(&proxyruntime.Manager{})
	if imageClient.Proxy == nil {
		t.Fatal("SetProxyManager did not attach the manager")
	}
	imageClient.SetProxyManager(nil)
	if imageClient.Proxy != nil {
		t.Fatal("SetProxyManager(nil) must clear the manager")
	}
}

func TestBuildProofTokenRejectsUnusableDifficulty(t *testing.T) {
	if got := buildProofToken("seed", "f", "ua", nil, ""); got != "" {
		t.Fatalf("odd-length difficulty must not yield a proof token: %q", got)
	}
	if got := buildProofToken("seed", "", "ua", nil, ""); got != "" {
		t.Fatalf("empty difficulty must not yield a proof token: %q", got)
	}
	if got := buildProofToken("seed", "zzzz", "ua", nil, ""); got != "" {
		t.Fatalf("non-hex difficulty must not yield a proof token: %q", got)
	}
}

func TestBuildProofTokenSatisfiesSingleByteDifficulty(t *testing.T) {
	// A one-byte target is satisfied as soon as the first hash byte is at most
	// the target, so the search terminates almost immediately.
	token := buildProofToken("seed", "ff", openAIUserAgent, []string{"https://chatgpt.com/sdk.js"}, "build-1")
	if !strings.HasPrefix(token, "gAAAAAB") {
		t.Fatalf("unexpected proof token prefix: %q", token)
	}
	encoded := strings.TrimPrefix(token, "gAAAAAB")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		t.Fatalf("proof token payload is not base64: err=%v", err)
	}
	if !strings.Contains(string(raw), "https://chatgpt.com/sdk.js") || !strings.Contains(string(raw), "build-1") {
		t.Fatalf("proof token payload lost the sentinel inputs: %s", raw)
	}
}

func TestOpenAIImagePollErrorSummaryClassifiesEveryFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "nil", err: nil, expected: "none"},
		{name: "deadline", err: context.DeadlineExceeded, expected: "request deadline exceeded"},
		{name: "upstream", err: &protocol.UpstreamError{Status: http.StatusBadGateway, Message: "upstream"}, expected: "upstream HTTP 502"},
		{name: "eof", err: errors.New("read: unexpected EOF"), expected: "unexpected EOF"},
		{name: "reset", err: errors.New("read: connection reset by peer"), expected: "connection reset by peer"},
		{name: "timeout", err: errors.New("dial tcp: i/o timeout"), expected: "network timeout"},
		{name: "decode", err: errors.New("decode image conversation response: invalid character"), expected: "invalid upstream JSON"},
		{name: "no reference", err: errors.New("upstream response contained no image reference"), expected: "upstream response contained no image reference"},
		{name: "unknown", err: errors.New("payment required"), expected: "poll request failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := openAIImagePollErrorSummary(test.err); got != test.expected {
				t.Fatalf("openAIImagePollErrorSummary(%v) = %q, want %q", test.err, got, test.expected)
			}
		})
	}
}

func TestOpenAIImageTerminalReasonReadsStatusAndRefusalShapes(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{name: "status failed", value: map[string]any{"status": "Failed"}, expected: "failed"},
		{name: "state cancelled", value: map[string]any{"state": " CANCELLED "}, expected: "cancelled"},
		{name: "task status", value: map[string]any{"task_status": "moderated"}, expected: "moderated"},
		{name: "generation status", value: map[string]any{"generation_status": "finished_error"}, expected: "finished_error"},
		{name: "finish reason", value: map[string]any{"finish_reason": "CONTENT_FILTER"}, expected: "content_filter"},
		{name: "content type refusal", value: map[string]any{"content_type": "refusal"}, expected: "refusal"},
		{name: "finish type safety", value: map[string]any{"finish_type": "safety"}, expected: "safety"},
		{name: "error message", value: map[string]any{"error": "policy violation"}, expected: "policy violation"},
		{name: "error code", value: map[string]any{"error": map[string]any{"code": "moderation_blocked"}}, expected: "moderation_blocked"},
		{name: "error type", value: map[string]any{"error": map[string]any{"type": "invalid_request"}}, expected: "invalid_request"},
		{name: "nested slice", value: []any{map[string]any{"nested": map[string]any{"status": "rejected"}}}, expected: "rejected"},
		{name: "non terminal status", value: map[string]any{"status": "in_progress"}, expected: ""},
		{name: "unrelated finish reason", value: map[string]any{"finish_reason": "stop"}, expected: ""},
		{name: "blank error", value: map[string]any{"error": "   "}, expected: ""},
		{name: "empty error object", value: map[string]any{"error": map[string]any{}}, expected: ""},
		{name: "string payload", value: "plain text", expected: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := openAIImageTerminalReason(test.value); got != test.expected {
				t.Fatalf("openAIImageTerminalReason(%#v) = %q, want %q", test.value, got, test.expected)
			}
		})
	}
}

func TestOpenAIImageTerminalErrorReturnsNilWithoutReason(t *testing.T) {
	if err := openAIImageTerminalError(map[string]any{"mapping": map[string]any{}}); err != nil {
		t.Fatalf("expected no terminal error, got %v", err)
	}
}

func TestTruncateOpenAIImageReasonCollapsesWhitespaceAndCapsLength(t *testing.T) {
	if got := truncateOpenAIImageReason("  policy   violation\n\tdetected  "); got != "policy violation detected" {
		t.Fatalf("whitespace was not collapsed: %q", got)
	}
	if got := truncateOpenAIImageReason("short reason"); got != "short reason" {
		t.Fatalf("short reason was altered: %q", got)
	}
	long := truncateOpenAIImageReason(strings.Repeat("a", 400))
	if len(long) != 160 {
		t.Fatalf("expected a 160 character cap, got %d", len(long))
	}
	truncated := openAIImageTerminalReason(map[string]any{"error": map[string]any{"message": strings.Repeat("b", 300)}})
	if len(truncated) != 160 {
		t.Fatalf("terminal reason was not truncated, got %d characters", len(truncated))
	}
}

func TestBrowserForReusesOneClientPerProxy(t *testing.T) {
	imageClient := NewOpenAIImage("https://chatgpt.com", nil, nil, 2*time.Second)
	first, err := imageClient.browserFor("http://browser.invalid:8080")
	if err != nil {
		t.Fatal(err)
	}
	second, err := imageClient.browserFor("http://browser.invalid:8080")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("browserFor created a second client for the same proxy")
	}
	t.Cleanup(func() { first.CloseIdleConnections() })

	// A different proxy must get its own client so egress stays isolated.
	other, err := imageClient.browserFor("http://other.invalid:8080")
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("browserFor shared a client across different proxies")
	}
	other.CloseIdleConnections()

	// A sub-second timeout is rounded up to the 30 second browser floor.
	floored := NewOpenAIImage("https://chatgpt.com", nil, nil, 100*time.Millisecond)
	flooredBrowser, err := floored.browserFor("")
	if err != nil {
		t.Fatal(err)
	}
	flooredBrowser.CloseIdleConnections()
}

func TestBrowserForSurfacesConstructionFailure(t *testing.T) {
	imageClient := NewOpenAIImage("https://chatgpt.com", nil, nil, time.Second)
	if _, err := imageClient.browserFor("://not-a-proxy-url"); err == nil {
		t.Fatal("expected an invalid proxy URL to fail client construction")
	}
}

func TestBrowserForUsesDefaultTimeoutWhenUnset(t *testing.T) {
	imageClient := NewOpenAIImage("https://chatgpt.com", nil, nil, 0)
	browser, err := imageClient.browserFor("")
	if err != nil {
		t.Fatal(err)
	}
	defer browser.CloseIdleConnections()
}

func TestMinDurationPicksTheShorterBound(t *testing.T) {
	if got := minDuration(0, 20*time.Second); got != 20*time.Second {
		t.Fatalf("an unset timeout must fall back to the bound, got %s", got)
	}
	if got := minDuration(-time.Second, 20*time.Second); got != 20*time.Second {
		t.Fatalf("a negative timeout must fall back to the bound, got %s", got)
	}
	if got := minDuration(5*time.Second, 20*time.Second); got != 5*time.Second {
		t.Fatalf("the smaller timeout must win, got %s", got)
	}
	if got := minDuration(30*time.Second, 20*time.Second); got != 20*time.Second {
		t.Fatalf("the bound must cap a larger timeout, got %s", got)
	}
	if got := minDuration(20*time.Second, 20*time.Second); got != 20*time.Second {
		t.Fatalf("equal timeouts must be stable, got %s", got)
	}
}

func TestOpenAIImageModelNormalizesGptImageAlias(t *testing.T) {
	if got := openAIImageModel(" GPT-Image-2 "); got != "gpt-5-3" {
		t.Fatalf("gpt-image-2 was not normalized, got %q", got)
	}
	if got := openAIImageModel(" gpt-4o "); got != "gpt-4o" {
		t.Fatalf("other models must only be trimmed, got %q", got)
	}
}

func TestRequirementHeadersIncludeEverySentinelToken(t *testing.T) {
	imageClient := NewOpenAIImage("https://example.invalid", nil, nil, time.Second)
	headers := imageClient.requirementHeaders(openAIRequirements{Token: "token"})
	if headers["OpenAI-Sentinel-Chat-Requirements-Token"] != "token" {
		t.Fatalf("missing requirements token: %#v", headers)
	}
	for _, key := range []string{"OpenAI-Sentinel-Proof-Token", "OpenAI-Sentinel-Turnstile-Token", "OpenAI-Sentinel-SO-Token"} {
		if _, ok := headers[key]; ok {
			t.Fatalf("optional header %s must be omitted when empty", key)
		}
	}

	full := imageClient.requirementHeaders(openAIRequirements{
		Token:      "token",
		ProofToken: "proof",
		Turnstile:  "turnstile",
		SOToken:    "so",
	})
	if full["OpenAI-Sentinel-Proof-Token"] != "proof" || full["OpenAI-Sentinel-Turnstile-Token"] != "turnstile" || full["OpenAI-Sentinel-SO-Token"] != "so" {
		t.Fatalf("optional sentinel headers were dropped: %#v", full)
	}
	if full["Content-Type"] != "application/json" || full["Accept"] != "application/json" {
		t.Fatalf("unexpected base headers: %#v", full)
	}
}

func TestChatRequirementsBuildsProofTokenWhenUpstreamRequiresIt(t *testing.T) {
	var finalizeBody map[string]any
	server := newStubOpenAIImageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = writeJSON(w, map[string]any{
				"prepare_token": "prepare-token",
				"proofofwork":   map[string]any{"required": true, "seed": "seed", "difficulty": "ff"},
			})
		case "/backend-api/sentinel/chat-requirements/finalize":
			if err := readJSON(r, &finalizeBody); err != nil {
				t.Error(err)
			}
			_ = writeJSON(w, map[string]any{"token": "requirements-token", "so_token": "so-token"})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	requirements, err := client.chatRequirements(context.Background(), accounts.Account{Token: "jwt"}, []string{"https://chatgpt.com/sdk.js"}, "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if requirements.Token != "requirements-token" || requirements.SOToken != "so-token" {
		t.Fatalf("unexpected finalized requirements: %#v", requirements)
	}
	if !strings.HasPrefix(requirements.ProofToken, "gAAAAAB") {
		t.Fatalf("proof of work token was not built: %q", requirements.ProofToken)
	}
	if got := stringValue(finalizeBody["proof_token"]); !strings.HasPrefix(got, "gAAAAAB") {
		t.Fatalf("proof token was not sent to finalize: %q", got)
	}
	if stringValue(finalizeBody["turnstile_token"]) != "" {
		t.Fatalf("turnstile must stay empty when not required: %#v", finalizeBody)
	}
}

func TestChatRequirementsRejectsMissingFinalToken(t *testing.T) {
	server := newStubOpenAIImageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = writeJSON(w, map[string]any{"prepare_token": "prepare-token"})
		case "/backend-api/sentinel/chat-requirements/finalize":
			_ = writeJSON(w, map[string]any{})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	if _, err := client.chatRequirements(context.Background(), accounts.Account{Token: "jwt"}, nil, ""); err == nil ||
		!strings.Contains(err.Error(), "no token") {
		t.Fatalf("expected a missing token error, got %v", err)
	}
}

func TestChatRequirementsPropagatesPrepareFailure(t *testing.T) {
	server := newStubOpenAIImageServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "prepare unavailable", http.StatusServiceUnavailable)
	})
	defer server.Close()

	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	_, err := client.chatRequirements(context.Background(), accounts.Account{Token: "jwt"}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "prepare unavailable") {
		t.Fatalf("expected the prepare failure to surface, got %v", err)
	}
	var upstream *protocol.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusServiceUnavailable {
		t.Fatalf("unexpected error shape: %#v", upstream)
	}
}

func TestChatRequirementsRejectsMalformedFinalizeJSON(t *testing.T) {
	server := newStubOpenAIImageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = writeJSON(w, map[string]any{"prepare_token": "prepare-token"})
		default:
			w.Write([]byte("not json"))
		}
	})
	defer server.Close()

	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	if _, err := client.chatRequirements(context.Background(), accounts.Account{Token: "jwt"}, nil, ""); err == nil ||
		!strings.Contains(err.Error(), "chat requirements finalize") {
		t.Fatalf("expected a decode failure for finalize, got %v", err)
	}
}
