package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
	"github.com/auucoder/gptgrok2api-go/internal/protocol"
	proxyruntime "github.com/auucoder/gptgrok2api-go/internal/proxy"
)

func TestOpenAIImageHelperFunctions(t *testing.T) {
	t.Run("isOpenAIImageFileID", func(t *testing.T) {
		if !isOpenAIImageFileID("file_abc123") {
			t.Error("file_abc123 should be valid")
		}
		if !isOpenAIImageFileID("file-service://file_abc123") {
			t.Error("file-service://file_abc123 should be valid")
		}
		if isOpenAIImageFileID("not_a_file_id") {
			t.Error("not_a_file_id should be invalid")
		}
		if isOpenAIImageFileID("") {
			t.Error("empty string should be invalid")
		}
		if isOpenAIImageFileID("file-service://") {
			t.Error("prefix only should be invalid")
		}
		if isOpenAIImageFileID("abc") {
			t.Error("too short should be invalid")
		}
	})

	t.Run("imageDimensions valid PNG", func(t *testing.T) {
		var buffer bytes.Buffer
		canvas := image.NewRGBA(image.Rect(0, 0, 3, 5))
		if err := png.Encode(&buffer, canvas); err != nil {
			t.Fatal(err)
		}
		w, h := imageDimensions(buffer.Bytes())
		if w != 3 || h != 5 {
			t.Fatalf("got %dx%d, want 3x5", w, h)
		}
	})

	t.Run("imageDimensions invalid data", func(t *testing.T) {
		w, h := imageDimensions([]byte("not an image"))
		if w != 1024 || h != 1024 {
			t.Fatalf("fallback got %dx%d, want 1024x1024", w, h)
		}
	})

	t.Run("randomMediaID returns 32 hex chars", func(t *testing.T) {
		id := randomMediaID()
		if len(id) != 32 {
			t.Fatalf("got len %d, want 32", len(id))
		}
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Fatalf("unexpected char %q in media id %q", c, id)
			}
		}
	})

	t.Run("isRetryableOpenAIError", func(t *testing.T) {
		if !isRetryableOpenAIError(fmt.Errorf("network timeout")) {
			t.Error("generic error should be retryable")
		}
		for _, status := range []int{404, 409, 429, 500, 502, 503} {
			if !isRetryableOpenAIError(&protocol.UpstreamError{Status: status}) {
				t.Errorf("status %d should be retryable", status)
			}
		}
		for _, status := range []int{400, 401, 403, 418} {
			if isRetryableOpenAIError(&protocol.UpstreamError{Status: status}) {
				t.Errorf("status %d should not be retryable", status)
			}
		}
	})

	t.Run("selectedProxyURL", func(t *testing.T) {
		client := &http.Client{}
		o := NewOpenAIImage("https://chatgpt.com", client, nil, 5*time.Second)

		ctxWithProxy := proxyruntime.WithURL(context.Background(), "http://proxy.invalid:8080")
		if got := o.selectedProxyURL(ctxWithProxy, accounts.Account{}); got != "http://proxy.invalid:8080" {
			t.Fatalf("expected ctx proxy, got %q", got)
		}

		ctxNoProxy := context.Background()
		if got := o.selectedProxyURL(ctxNoProxy, accounts.Account{}); got != "" {
			t.Fatalf("expected empty proxy, got %q", got)
		}
	})

	t.Run("doAbsolute", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("absolute"))
		}))
		defer server.Close()

		o := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)

		got, err := o.doAbsolute(context.Background(), http.MethodGet, server.URL+"/x", accounts.Account{}, nil, nil, false)
		if err != nil {
			t.Fatalf("doAbsolute: %v", err)
		}
		defer got.Body.Close()
		raw, _ := io.ReadAll(got.Body)
		if string(raw) != "absolute" {
			t.Fatalf("got %q", raw)
		}

		if _, err := o.doAbsolute(context.Background(), http.MethodGet, "http://bad url with spaces", accounts.Account{}, nil, nil, false); err == nil {
			t.Error("expected parse error")
		}
	})
}

func TestOpenAIImageResolve(t *testing.T) {
	pngBytes := onePixelPNGForTest(t)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	dir := t.TempDir()

	o := NewOpenAIImage("https://chatgpt.com", &http.Client{}, nil, 5*time.Second)

	t.Run("valid url format", func(t *testing.T) {
		maps, value, err := o.Resolve(context.Background(), accounts.Account{}, ImageResult{Base64: b64, MIME: "image/png"}, "", dir, "http://public.example")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if value != maps["url"] {
			t.Fatalf("value and maps[url] differ: %q vs %q", value, maps["url"])
		}
		if !strings.HasPrefix(value, "http://public.example/v1/files/image?id=") {
			t.Fatalf("value = %q", value)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			t.Fatalf("expected written media file in %s", dir)
		}
	})

	t.Run("b64_json format returns b64 map and url value", func(t *testing.T) {
		maps, value, err := o.Resolve(context.Background(), accounts.Account{}, ImageResult{Base64: b64, MIME: "image/png"}, "b64_json", dir, "")
		if err != nil {
			t.Fatalf("Resolve b64: %v", err)
		}
		if maps["b64_json"] != b64 {
			t.Fatal("b64_json key missing")
		}
		if !strings.HasPrefix(value, "/v1/files/image?id=") {
			t.Fatalf("value = %q", value)
		}
	})

	t.Run("empty Base64 errors", func(t *testing.T) {
		if _, _, err := o.Resolve(context.Background(), accounts.Account{}, ImageResult{}, "", dir, ""); err == nil {
			t.Fatal("expected error for empty Base64")
		}
	})

	t.Run("invalid base64 errors", func(t *testing.T) {
		if _, _, err := o.Resolve(context.Background(), accounts.Account{}, ImageResult{Base64: "!!!"}, "", dir, ""); err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("invalid format errors", func(t *testing.T) {
		if _, _, err := o.Resolve(context.Background(), accounts.Account{}, ImageResult{Base64: b64}, "bogus", dir, ""); err == nil {
			t.Fatal("expected format error")
		}
	})
}

func TestOpenAIImageReadImageDownloadResponse(t *testing.T) {
	t.Run("json redirect to download url", func(t *testing.T) {
		downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("binary-image-bytes"))
		}))
		defer downloadServer.Close()

		metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"download_url": downloadServer.URL})
		}))
		defer metaServer.Close()

		client := &http.Client{}
		o := NewOpenAIImage(metaServer.URL, client, nil, 5*time.Second)

		resp := &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"download_url":"` + downloadServer.URL + `"}`)),
		}
		got, mime, err := o.readImageDownloadResponse(context.Background(), accounts.Account{}, resp)
		if err != nil {
			t.Fatalf("readImageDownloadResponse: %v", err)
		}
		if string(got) != "binary-image-bytes" {
			t.Fatalf("got %q", got)
		}
		if mime != "image/png" {
			t.Fatalf("mime = %q, want image/png", mime)
		}
	})

	t.Run("direct binary response", func(t *testing.T) {
		client := &http.Client{}
		o := NewOpenAIImage("https://chatgpt.com", client, nil, 5*time.Second)
		resp := &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader([]byte("direct-bytes"))),
		}
		got, mime, err := o.readImageDownloadResponse(context.Background(), accounts.Account{}, resp)
		if err != nil {
			t.Fatalf("direct: %v", err)
		}
		if string(got) != "direct-bytes" {
			t.Fatalf("got %q", got)
		}
		if mime != "image/png" {
			t.Fatalf("default mime = %q, want image/png", mime)
		}
	})

	t.Run("empty content errors", func(t *testing.T) {
		client := &http.Client{}
		o := NewOpenAIImage("https://chatgpt.com", client, nil, 5*time.Second)
		resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
		if _, _, err := o.readImageDownloadResponse(context.Background(), accounts.Account{}, resp); err == nil {
			t.Fatal("expected empty content error")
		}
	})

	t.Run("json with no url errors", func(t *testing.T) {
		client := &http.Client{}
		o := NewOpenAIImage("https://chatgpt.com", client, nil, 5*time.Second)
		resp := &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"foo":"bar"}`)),
		}
		if _, _, err := o.readImageDownloadResponse(context.Background(), accounts.Account{}, resp); err == nil {
			t.Fatal("expected no URL error")
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		client := &http.Client{}
		o := NewOpenAIImage("https://chatgpt.com", client, nil, 5*time.Second)
		resp := &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{invalid json`)),
		}
		if _, _, err := o.readImageDownloadResponse(context.Background(), accounts.Account{}, resp); err == nil {
			t.Fatal("expected json decode error")
		}
	})
}

func TestMediaUploadAndFetchBranches(t *testing.T) {
	t.Run("Upload rejects invalid base64", func(t *testing.T) {
		media := NewMedia(&http.Client{}, "", "", "", "", 5*time.Second)
		if _, _, err := media.Upload(context.Background(), accounts.Account{}, "f", "image/png", "!!!not-base64!!!"); err == nil {
			t.Fatal("expected base64 error")
		}
	})

	t.Run("Upload falls back to fileId", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"fileId": "fb-id"})
		}))
		defer server.Close()
		media := NewMedia(server.Client(), "", "", server.URL, "", 5*time.Second)
		encoded := base64.StdEncoding.EncodeToString([]byte("x"))
		id, _, err := media.Upload(context.Background(), accounts.Account{Token: "sso=t"}, "f", "image/png", encoded)
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if id != "fb-id" {
			t.Fatalf("id = %q, want fb-id", id)
		}
	})

	t.Run("Fetch errors on non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", 500)
		}))
		defer server.Close()
		media := NewMedia(server.Client(), "", "", "", "", 5*time.Second)
		if _, _, err := media.Fetch(context.Background(), accounts.Account{Token: "sso=t"}, server.URL+"/asset"); err == nil {
			t.Fatal("expected HTTP error")
		}
	})

	t.Run("Fetch errors on empty body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))
		defer server.Close()
		media := NewMedia(server.Client(), "", "", "", "", 5*time.Second)
		if _, _, err := media.Fetch(context.Background(), accounts.Account{Token: "sso=t"}, server.URL+"/asset"); err == nil {
			t.Fatal("expected empty content error")
		}
	})

	t.Run("Fetch with proxy manager", func(t *testing.T) {
		manager := proxyruntime.NewManager("", nil)
		media := NewMedia(&http.Client{}, "", "", "", "", 5*time.Second)
		media.SetProxyManager(manager)
		if media.Proxy == nil {
			t.Fatal("SetProxyManager did not set proxy")
		}
	})
}

func TestSentinelTurnstileBranches(t *testing.T) {
	if _, err := solveSentinelTurnstileToken("!!!bad-base64!!!", "key"); err == nil {
		t.Error("expected base64 decode error")
	}

	if _, err := solveSentinelTurnstileToken(base64.StdEncoding.EncodeToString([]byte("not-json")), "key"); err == nil {
		t.Error("expected unmarshal error")
	}

	raw, _ := json.Marshal([]any{float64(1)})
	encoded := base64.StdEncoding.EncodeToString(raw)
	if _, err := solveSentinelTurnstileToken(encoded, ""); err == nil {
		t.Error("expected no-token error for trivial queue")
	}
}

func TestGrokQuotaRefreshTokenBranches(t *testing.T) {
	t.Run("RefreshToken happy path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"remainingQueries":  10,
				"totalQueries":      100,
				"windowSizeSeconds": 3600,
			})
		}))
		defer server.Close()
		quota := NewGrokQuota(server.URL, server.Client(), nil)
		result, err := quota.RefreshToken(context.Background(), "old-tok", nil)
		if err != nil {
			t.Fatalf("RefreshToken: %v", err)
		}
		if len(result) == 0 {
			t.Fatal("expected quotas")
		}
	})

	t.Run("RefreshToken HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "denied", 403)
		}))
		defer server.Close()
		quota := NewGrokQuota(server.URL, server.Client(), nil)
		if _, err := quota.RefreshToken(context.Background(), "old-tok", nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestOpenAIImageUploadInputBranches(t *testing.T) {
	t.Run("uploadInput empty data error", func(t *testing.T) {
		client := &http.Client{}
		o := NewOpenAIImage("https://chatgpt.invalid", client, nil, 5*time.Second)
		_, err := o.uploadInput(context.Background(), accounts.Account{}, OpenAIImageInput{}, 0)
		if err == nil {
			t.Fatal("expected empty data error")
		}
	})

	t.Run("uploadInput with valid data", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/backend-api/files":
				_ = json.NewEncoder(w).Encode(map[string]any{"file_id": "file_test", "upload_url": server.URL + "/blob"})
			case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blob"):
				w.WriteHeader(http.StatusCreated)
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/uploaded"):
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		o := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
		input := OpenAIImageInput{Name: "test.png", MIME: "image/png", Data: onePixelPNGForTest(t)}
		_, err := o.uploadInput(context.Background(), accounts.Account{}, input, 0)
		if err != nil {
			t.Fatalf("uploadInput: %v", err)
		}
	})
}

func TestMediaResolveImageBranches(t *testing.T) {
	t.Run("b64_json with Base64 shortcut", func(t *testing.T) {
		media := &Media{}
		result, err := media.ResolveImage(context.Background(), accounts.Account{}, ImageResult{Base64: "cGgt"}, "b64_json", t.TempDir(), "")
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if result["b64_json"] != "cGgt" {
			t.Fatalf("got %q", result["b64_json"])
		}
	})

	t.Run("url format writes file", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-bytes"))
		}))
		defer server.Close()

		media := NewMedia(server.Client(), "", "", "", "", 5*time.Second)
		dir := t.TempDir()
		result, err := media.ResolveImage(context.Background(), accounts.Account{Token: "sso=t"}, ImageResult{URL: server.URL + "/x.png"}, "url", dir, "http://pub.example")
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if !strings.HasPrefix(result["url"], "http://pub.example/v1/files/image?id=") {
			t.Fatalf("url = %q", result["url"])
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			t.Fatalf("expected written media file in %s", dir)
		}
	})

	t.Run("b64_json fetches when no Base64", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpg-bytes"))
		}))
		defer server.Close()

		media := NewMedia(server.Client(), "", "", "", "", 5*time.Second)
		result, err := media.ResolveImage(context.Background(), accounts.Account{Token: "sso=t"}, ImageResult{URL: server.URL + "/x.jpg"}, "b64_json", t.TempDir(), "")
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if result["b64_json"] == "" || result["mime"] != "image/jpeg" {
			t.Fatalf("got %#v", result)
		}
	})
}

func TestImageResolveInvalidFormatAndEmptyDir(t *testing.T) {
	t.Run("invalid format errors", func(t *testing.T) {
		media := &Media{}
		if _, err := media.ResolveImage(context.Background(), accounts.Account{}, ImageResult{}, "invalid", "", ""); err == nil {
			t.Fatal("expected format error")
		}
	})
}

func TestOpenAIImagePrepare(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/backend-api/files" {
			_ = json.NewEncoder(w).Encode(map[string]any{"file_id": "file_prep", "upload_url": server.URL + "/upload"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	o := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	meta, err := o.prepareInputUpload(context.Background(), accounts.Account{}, map[string]any{"file_name": "test.png"})
	if err != nil {
		t.Fatalf("prepareInputUpload: %v", err)
	}
	if stringValue(meta["file_id"]) != "file_prep" {
		t.Fatalf("file_id = %q", meta["file_id"])
	}
}

func onePixelPNGForTest(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 1, 1))
	canvas.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
