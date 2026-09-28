package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
)

func TestMediaPublicMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: done\n\n"))
		case "/post":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "post1"})
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]any{"fileId": "fid", "fileUri": "uri"})
		case "/test.png":
			_, _ = w.Write([]byte("binary content"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	media := NewMedia(server.Client(), server.URL+"/chat", server.URL+"/post", server.URL+"/upload", server.URL, 5*time.Second)
	account := accounts.Account{Token: "sso=tok"}

	t.Run("StreamChat", func(t *testing.T) {
		resp, err := media.StreamChat(context.Background(), account, map[string]any{})
		if err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
		resp.Body.Close()
	})

	t.Run("CreatePost", func(t *testing.T) {
		result, err := media.CreatePost(context.Background(), account, "image/jpeg", "https://example.com/img.jpg", "test prompt")
		if err != nil {
			t.Fatalf("CreatePost: %v", err)
		}
		if result["id"] != "post1" {
			t.Fatalf("CreatePost id = %v", result["id"])
		}
	})

	t.Run("Upload", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString([]byte("fake image data"))
		fileID, uri, err := media.Upload(context.Background(), account, "test.jpg", "image/jpeg", encoded)
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if fileID != "fid" || uri != "uri" {
			t.Fatalf("Upload = %s, %s", fileID, uri)
		}
	})

	t.Run("Fetch", func(t *testing.T) {
		data, mime, err := media.Fetch(context.Background(), account, "/test.png")
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if string(data) != "binary content" {
			t.Fatalf("Fetch data = %q", string(data))
		}
		if mime == "" {
			t.Fatal("Fetch mime should not be empty")
		}
	})
}

func TestMediaGenerateLite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 1; i <= 2; i++ {
			payload := map[string]any{"streamingImageGenerationResponse": map[string]any{
				"imageUrl": fmt.Sprintf("https://assets.grok.com/img%d.png", i),
				"imageId":  fmt.Sprintf("id-%d", i),
			}}
			raw, _ := json.Marshal(payload)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	media := NewMedia(server.Client(), server.URL, server.URL, server.URL, server.URL, 5*time.Second)
	results, err := media.GenerateLite(context.Background(), accounts.Account{Token: "sso=tok"}, "a cat", "fast", 2)
	if err != nil {
		t.Fatalf("GenerateLite: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 images, got %d", len(results))
	}
}

func TestMediaGenerateLiteEmptyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	media := NewMedia(server.Client(), server.URL, server.URL, server.URL, server.URL, 5*time.Second)
	if _, err := media.GenerateLite(context.Background(), accounts.Account{Token: "sso=tok"}, "a cat", "fast", 1); err == nil {
		t.Fatal("stream without images should error")
	}
}

func TestImageResolveImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image binary"))
	}))
	defer server.Close()

	media := NewMedia(server.Client(), server.URL, server.URL, server.URL, server.URL, 5*time.Second)
	image := ImageResult{URL: server.URL + "/img.png", Base64: ""}

	t.Run("url format", func(t *testing.T) {
		result, err := media.ResolveImage(context.Background(), accounts.Account{}, image, "url", "/tmp", "")
		if err != nil {
			t.Fatalf("ResolveImage url: %v", err)
		}
		if result["url"] == "" {
			t.Fatal("url format should return url")
		}
	})

	t.Run("b64_json format", func(t *testing.T) {
		result, err := media.ResolveImage(context.Background(), accounts.Account{}, image, "b64_json", "/tmp", "")
		if err != nil {
			t.Fatalf("ResolveImage b64_json: %v", err)
		}
		if result["b64_json"] == "" {
			t.Fatal("b64_json format should return base64")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := media.ResolveImage(context.Background(), accounts.Account{}, image, "invalid", "/tmp", "")
		if err == nil {
			t.Fatal("invalid format should error")
		}
	})
}

func TestImageResultBase64Shortcut(t *testing.T) {
	media := &Media{}
	image := ImageResult{URL: "ignored", Base64: "precomputed"}
	result, err := media.ResolveImage(context.Background(), accounts.Account{}, image, "b64_json", "/tmp", "")
	if err != nil {
		t.Fatalf("ResolveImage with Base64 shortcut: %v", err)
	}
	if result["b64_json"] != "precomputed" {
		t.Fatalf("Base64 shortcut failed: %v", result)
	}
}

func TestGrokQuotaRefresh(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"remainingQueries":   100,
			"totalQueries":       200,
			"windowSizeSeconds":  72000,
		})
	}))
	defer server.Close()

	quota := NewGrokQuota(server.URL, server.Client(), nil)
	account := accounts.Account{Token: "tok", Fields: map[string]any{"cookie_header": "sso=tok"}}

	result, err := quota.Refresh(context.Background(), account)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if hits.Load() == 0 {
		t.Fatal("quota refresh endpoint not called")
	}
	if _, ok := result["auto"]; !ok {
		t.Fatalf("expected auto quota in result, got keys: %v", result)
	}
}

func TestXAIProbeTestValidation(t *testing.T) {
	probe := NewXAIProbe("https://api.x.ai", "https://auth.x.ai", &http.Client{Timeout: 5 * time.Second})

	t.Run("missing access token", func(t *testing.T) {
		result := probe.Test(context.Background(), map[string]any{}, "model", "prompt")
		if result.Status != "invalid" {
			t.Fatalf("expected invalid status, got %s", result.Status)
		}
		if result.Code != "invalid_credentials" {
			t.Fatalf("expected invalid_credentials code, got %s", result.Code)
		}
	})

	t.Run("with valid token mocks", func(t *testing.T) {
		// This will fail on actual HTTP but validates the token presence check path
		result := probe.Test(context.Background(), map[string]any{"access_token": "tok"}, "", "")
		// Status depends on actual HTTP response, but we validated the token path was taken
		if result.AccessToken != "tok" {
			t.Fatalf("accessToken not propagated: %s", result.AccessToken)
		}
	})
}

func TestXAIProbeRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new_access",
			"refresh_token": "new_refresh",
			"id_token": "new_id",
		})
	}))
	defer server.Close()

	probe := NewXAIProbe("https://api.x.ai", server.URL, &http.Client{Timeout: 5 * time.Second})
	tokens, err := probe.refresh(context.Background(), "old_refresh")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tokens.AccessToken != "new_access" {
		t.Fatalf("access token = %s", tokens.AccessToken)
	}
}

func TestSentinelVMProperty(t *testing.T) {
	vm := &sentinelVM{}

	t.Run("ordered map property", func(t *testing.T) {
		om := &sentinelOrderedMap{
			keys:   []string{"a", "b"},
			values: map[string]any{"a": 1, "b": 2},
		}
		if got := vm.property(om, "a"); got != 1 {
			t.Fatalf("ordered map property = %v", got)
		}
	})

	t.Run("map property", func(t *testing.T) {
		m := map[string]any{"key": "value"}
		if got := vm.property(m, "key"); got != "value" {
			t.Fatalf("map property = %v", got)
		}
	})

	t.Run("array property", func(t *testing.T) {
		arr := []any{"zero", "one", "two"}
		if got := vm.property(arr, float64(1)); got != "one" {
			t.Fatalf("array property = %v", got)
		}
		if got := vm.property(arr, float64(99)); got != nil {
			t.Fatalf("out of bounds should be nil, got %v", got)
		}
	})

	t.Run("string property", func(t *testing.T) {
		if got := vm.property("window.document", "location"); got != "https://chatgpt.com/" {
			t.Fatalf("document.location = %v", got)
		}
		if got := vm.property("obj", "method"); got != "obj.method" {
			t.Fatalf("string method = %v", got)
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		if got := vm.property(42, "key"); got != nil {
			t.Fatalf("number property should be nil, got %v", got)
		}
	})
}

func TestSentinelVMCallTarget(t *testing.T) {
	vm := &sentinelVM{startTime: time.Now()}

	t.Run("callable sentinel", func(t *testing.T) {
		fn := sentinelCallable(func(args ...any) any { return "called" })
		if got := vm.callTarget(fn); got != "called" {
			t.Fatalf("callable = %v", got)
		}
	})

	t.Run("performance.now", func(t *testing.T) {
		result := vm.callTarget("window.performance.now")
		if val, ok := result.(float64); !ok || val < 0 {
			t.Fatalf("performance.now = %v", result)
		}
	})

	t.Run("Object.create", func(t *testing.T) {
		result := vm.callTarget("window.Object.create")
		if _, ok := result.(*sentinelOrderedMap); !ok {
			t.Fatalf("Object.create = %T", result)
		}
	})

	t.Run("Object.keys string", func(t *testing.T) {
		result := vm.callTarget("window.Object.keys", "window.localStorage")
		list, ok := result.([]any)
		if !ok {
			t.Fatalf("Object.keys string = %T", result)
		}
		if len(list) == 0 {
			t.Fatal("localStorage keys should not be empty")
		}
	})

	t.Run("Object.keys ordered map", func(t *testing.T) {
		om := &sentinelOrderedMap{keys: []string{"x", "y"}, values: map[string]any{"x": 1, "y": 2}}
		result := vm.callTarget("window.Object.keys", om)
		list, ok := result.([]any)
		if !ok {
			t.Fatalf("Object.keys om = %T", result)
		}
		if len(list) != 2 {
			t.Fatalf("Object.keys ordered map length = %d", len(list))
		}
	})

	t.Run("Object.keys map", func(t *testing.T) {
		m := map[string]any{"a": 1}
		result := vm.callTarget("window.Object.keys", m)
		list, ok := result.([]any)
		if !ok {
			t.Fatalf("Object.keys map = %T", result)
		}
		if len(list) != 1 {
			t.Fatalf("Object.keys map length = %d", len(list))
		}
	})

	t.Run("Reflect.set ordered map", func(t *testing.T) {
		om := &sentinelOrderedMap{values: map[string]any{}}
		result := vm.callTarget("window.Reflect.set", om, "newKey", "newValue")
		if result != true {
			t.Fatalf("Reflect.set om = %v", result)
		}
		if om.values["newKey"] != "newValue" {
			t.Fatalf("Reflect.set om values = %v", om.values)
		}
	})

	t.Run("Reflect.set map", func(t *testing.T) {
		m := map[string]any{}
		result := vm.callTarget("window.Reflect.set", m, "k", "v")
		if result != true {
			t.Fatalf("Reflect.set map = %v", result)
		}
		if m["k"] != "v" {
			t.Fatalf("Reflect.set map = %v", m)
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		result := vm.callTarget("unknown.target")
		if result != nil {
			t.Fatalf("unknown target = %v", result)
		}
	})
}

func TestSentinelVMCallTargetChecked(t *testing.T) {
	vm := &sentinelVM{}

	fn := sentinelCallable(func(args ...any) any { return "ok" })
	result, err := vm.callTargetChecked(fn, nil)
	if err != nil {
		t.Fatalf("callTargetChecked: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %v", result)
	}

	result, err = vm.callTargetChecked("unknown", nil)
	if err != nil {
		t.Fatalf("callTargetChecked unknown: %v", err)
	}
	if result != nil {
		t.Fatalf("unknown result = %v", result)
	}
}

func TestNewXAIProbe(t *testing.T) {
	probe := NewXAIProbe("  https://api.x.ai/  ", "  https://auth.x.ai  ", nil)
	if probe.BaseURL != "https://api.x.ai" {
		t.Fatalf("BaseURL = %q", probe.BaseURL)
	}
	if probe.TokenURL != "https://auth.x.ai" {
		t.Fatalf("TokenURL = %q", probe.TokenURL)
	}
	if probe.HTTP == nil {
		t.Fatal("HTTP client should be initialized")
	}
}

func TestNewMedia(t *testing.T) {
	media := NewMedia(nil, "http://a", "http://b", "http://c", "http://d", 100)
	if media.ImageChatURL != "http://a" {
		t.Fatalf("ImageChatURL = %q", media.ImageChatURL)
	}
	if media.Client == nil {
		t.Fatal("Client should be initialized")
	}
}
