package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/auucoder/gptgrok2api-go/internal/accounts"
)

func TestCleanMIME(t *testing.T) {
	cases := []struct{ in, want string }{
		{"application/vnd.ms-powerpoint; charset=binary", "application/vnd.ms-powerpoint"},
		{"  IMAGE/PNG  ", "image/png"},
		{"text/plain", "text/plain"},
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanMIME(c.in); got != c.want {
			t.Fatalf("cleanMIME(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestEditableExtension(t *testing.T) {
	if got := editableExtension("ppt", "application/octet-stream"); got != ".pptx" {
		t.Fatalf("ppt extension = %q", got)
	}
	if got := editableExtension("psd", "application/octet-stream"); got != ".psd" {
		t.Fatalf("psd extension = %q", got)
	}
	if got := editableExtension("zip", "application/octet-stream"); got != ".zip" {
		t.Fatalf("zip extension = %q", got)
	}
	if got := editableExtension("other", "text/plain"); got == "" {
		t.Fatal("text/plain should map to an extension")
	}
}

func TestSanitizeEditableName(t *testing.T) {
	if got := sanitizeEditableName("  /etc/../report.pptx  "); got != "report.pptx" {
		t.Fatalf("sanitizeEditableName = %q", got)
	}
	if got := sanitizeEditableName("my\x00file.zip"); got != "myfile.zip" {
		t.Fatalf("nul strip = %q", got)
	}
}

func TestContentDispositionName(t *testing.T) {
	if got := contentDispositionName(`attachment; filename="deck.pptx"`); got != "deck.pptx" {
		t.Fatalf("filename parse = %q", got)
	}
	if got := contentDispositionName("not a header"); got != "" {
		t.Fatalf("invalid header should return empty, got %q", got)
	}
}

func TestFirstNonEmptyProvider(t *testing.T) {
	if got := firstNonEmptyProvider("", "  ", "value", "later"); got != "value" {
		t.Fatalf("firstNonEmptyProvider = %q", got)
	}
	if got := firstNonEmptyProvider("", "   "); got != "" {
		t.Fatalf("all blank should be empty, got %q", got)
	}
}

func TestNestedString(t *testing.T) {
	value := map[string]any{
		"outer": map[string]any{"inner": []any{map[string]any{"target": "found"}}},
	}
	if got := nestedString(value, "target"); got != "found" {
		t.Fatalf("nested map search = %q", got)
	}
	if got := nestedString(`{"wrapped":{"target":"json"}}`, "target"); got != "json" {
		t.Fatalf("nested json string = %q", got)
	}
	if got := nestedString(value, "missing"); got != "" {
		t.Fatalf("missing key should be empty, got %q", got)
	}
}

func TestDecodeEditableInput(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"))
	got, err := decodeEditableInput("data:image/png;base64,"+encoded, 1)
	if err != nil {
		t.Fatalf("decodeEditableInput: %v", err)
	}
	if got.Name != "image_1.png" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.MIME != "image/png" {
		t.Fatalf("mime = %q", got.MIME)
	}

	if _, err := decodeEditableInput("!!!not-base64!!!", 2); err == nil {
		t.Fatal("expected decode error for invalid base64")
	} else if !strings.Contains(err.Error(), "image 2") {
		t.Fatalf("error should mention index, got %v", err)
	}
}

func TestDecodeEditableInputs(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hello"))
	items, err := DecodeEditableInputs([]string{encoded})
	if err != nil {
		t.Fatalf("DecodeEditableInputs: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 input, got %d", len(items))
	}
	if _, err := DecodeEditableInputs([]string{encoded, "@@@"}); err == nil {
		t.Fatal("expected error on second input")
	}
}

func TestCollectEditableArtifacts(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"id":     "m1",
				"author": map[string]any{"role": "assistant"},
				"attachments": []any{
					map[string]any{"file_id": "file-1", "name": "deck.pptx", "mime_type": "application/vnd.ms-powerpoint"},
					map[string]any{"file_id": "file-2", "name": "bundle.zip", "mime_type": "application/zip"},
				},
			},
		},
	}
	items := collectEditableArtifacts(payload)
	if len(items) < 2 {
		t.Fatalf("expected at least 2 artifacts, got %d: %+v", len(items), items)
	}
	byFile := map[string]editableArtifact{}
	for _, item := range items {
		if item.FileID != "" {
			byFile[item.FileID] = item
		}
	}
	if byFile["file-1"].MessageID != "m1" {
		t.Fatalf("file-1 message id = %q", byFile["file-1"].MessageID)
	}
	if byFile["file-1"].MIME != "application/vnd.ms-powerpoint" {
		t.Fatalf("file-1 mime = %q", byFile["file-1"].MIME)
	}
}

func TestMergeEditableArtifact(t *testing.T) {
	current := editableArtifact{FileID: "f1", Name: "old.zip"}
	latest := editableArtifact{Name: "new.zip", MIME: "application/zip"}
	merged := mergeEditableArtifact(current, latest)
	if merged.FileID != "f1" {
		t.Fatalf("should keep current file id, got %q", merged.FileID)
	}
	if merged.Name != "new.zip" {
		t.Fatalf("should take latest name, got %q", merged.Name)
	}
	if merged.MIME != "application/zip" {
		t.Fatalf("should take latest mime, got %q", merged.MIME)
	}
}

func TestPickEditableArtifacts(t *testing.T) {
	items := []editableArtifact{
		{FileID: "f1", Name: "deck.pptx"},
		{FileID: "f2", Name: "assets.zip", MIME: "application/zip"},
	}
	primary, archive := pickEditableArtifacts(items, "ppt")
	if primary == nil || primary.FileID != "f1" {
		t.Fatalf("ppt primary = %+v", primary)
	}
	if archive == nil || archive.FileID != "f2" {
		t.Fatalf("archive = %+v", archive)
	}

	psdPrimary, _ := pickEditableArtifacts([]editableArtifact{{FileID: "p1", Name: "layer.psd"}}, "psd")
	if psdPrimary == nil || psdPrimary.FileID != "p1" {
		t.Fatalf("psd primary = %+v", psdPrimary)
	}
	if got, _ := pickEditableArtifacts(items, "zip"); got != nil {
		t.Fatalf("zip kind should not set primary, got %+v", got)
	}
}

func TestFirstStringAndMIMEFromPath(t *testing.T) {
	if got := firstString(nil, "fallback"); got != "fallback" {
		t.Fatalf("firstString nil = %q", got)
	}
	if got := firstString("  value  ", "fallback"); got != "value" {
		t.Fatalf("firstString trim = %q", got)
	}
	if got := firstString("   ", "fallback"); got != "fallback" {
		t.Fatalf("blank should fall back, got %q", got)
	}

	cases := map[string]string{
		"https://cdn.test/a/b.png?x=1": "image/png",
		"https://cdn.test/a/b.webp":   "image/webp",
		"https://cdn.test/a/b.mp4":    "video/mp4",
		"https://cdn.test/a/b.webm":   "video/webm",
		"https://cdn.test/a/b.bin":    "image/jpeg",
	}
	for in, want := range cases {
		if got := mimeFromPath(in); got != want {
			t.Fatalf("mimeFromPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestReadHTTPError(t *testing.T) {
	response := &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("upstream exploded"))}
	err := readHTTPError(response)
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("expected body in error, got %v", err)
	}

	empty := &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("  "))}
	if err := readHTTPError(empty); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected status fallback, got %v", err)
	}
}

func TestApplyMediaHeaders(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://grok.com", nil)
	applyMediaHeaders(request, accounts.Account{Token: "sso=abc"}, "https://grok.com", "https://grok.com/")
	if got := request.Header.Get("Cookie"); got != "sso=abc; sso-rw=abc" {
		t.Fatalf("derived cookie = %q", got)
	}

	withCookie, _ := http.NewRequest(http.MethodGet, "https://grok.com", nil)
	applyMediaHeaders(withCookie, accounts.Account{Token: "tok", Fields: map[string]any{"cookie_header": "custom=1"}}, "https://grok.com", "https://grok.com/")
	if got := withCookie.Header.Get("Cookie"); got != "custom=1" {
		t.Fatalf("explicit cookie = %q", got)
	}
}

func TestMediaDoJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Mode") != "" {
			t.Errorf("unexpected header")
		}
		switch r.URL.Path {
		case "/ok":
			_ = json.NewEncoder(w).Encode(map[string]any{"hello": "world"})
		case "/bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("bad request body"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	media := NewMedia(server.Client(), server.URL, server.URL, server.URL, server.URL, 5*time.Second)
	account := accounts.Account{Token: "sso=abc"}

	value, err := media.doJSONMap(context.Background(), http.MethodGet, server.URL+"/ok", account, nil)
	if err != nil {
		t.Fatalf("doJSONMap: %v", err)
	}
	if value["hello"] != "world" {
		t.Fatalf("decoded value = %+v", value)
	}

	if _, err := media.doJSONMap(context.Background(), http.MethodGet, server.URL+"/bad", account, nil); err == nil {
		t.Fatal("expected error for 400 response")
	} else if !strings.Contains(err.Error(), "bad request body") {
		t.Fatalf("error should carry upstream body, got %v", err)
	}
}

func TestMediaDoJSONStreamHeader(t *testing.T) {
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		_, _ = w.Write([]byte("data: hi\n\n"))
	}))
	defer server.Close()

	media := NewMedia(server.Client(), server.URL, server.URL, server.URL, server.URL, 5*time.Second)
	response, err := media.doJSON(context.Background(), http.MethodPost, server.URL, accounts.Account{}, nil, true)
	if err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	defer response.Body.Close()
	if accept != "text/event-stream" {
		t.Fatalf("stream accept header = %q", accept)
	}
}

func TestCloneFHTTPHeader(t *testing.T) {
	source := http.Header{"Accept": []string{"application/json"}, "Cookie": []string{"a=1"}, "Host": []string{"grok.com"}}
	target := cloneFHTTPHeader(source)
	if len(target["Accept"]) != 1 || target["Accept"][0] != "application/json" {
		t.Fatalf("header copy failed: %+v", target)
	}
	order := target[fhttp.HeaderOrderKey]
	for _, key := range order {
		if key == "Cookie" || key == "Host" {
			t.Fatalf("order must exclude %q, got %v", key, order)
		}
	}

	if empty := cloneFHTTPHeader(http.Header{}); len(empty[fhttp.HeaderOrderKey]) != 0 {
		t.Fatal("empty header should not produce order key")
	}
}

func TestCloneStdHeader(t *testing.T) {
	source := fhttp.Header{
		"Content-Type":         []string{"application/json"},
		fhttp.HeaderOrderKey:   []string{"Content-Type"},
		fhttp.PHeaderOrderKey:  []string{"Content-Type"},
	}
	target := cloneStdHeader(source)
	if target.Get("Content-Type") != "application/json" {
		t.Fatalf("std header copy = %+v", target)
	}
	if _, ok := target[fhttp.HeaderOrderKey]; ok {
		t.Fatal("order keys must be stripped")
	}
}

func TestCloneStdHTTPResponse(t *testing.T) {
	source := &fhttp.Response{
		Status:        "200 OK",
		StatusCode:    200,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        fhttp.Header{"Content-Type": []string{"application/json"}},
		ContentLength: 2,
	}
	target := cloneStdHTTPResponse(source)
	if target.StatusCode != 200 || target.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("clone failed: %+v", target)
	}
	if target.Body == nil {
		t.Fatal("nil body should be replaced")
	}
	buffer := make([]byte, 4)
	if n, err := target.Body.Read(buffer); n != 0 || err != io.EOF {
		t.Fatalf("nil reader should EOF, got n=%d err=%v", n, err)
	}
}

func TestNewBrowserHTTP(t *testing.T) {
	client, err := newBrowserHTTP("", 5*time.Second)
	if err != nil {
		t.Fatalf("newBrowserHTTP: %v", err)
	}
	client.CloseIdleConnections()

	short, err := newBrowserHTTP("", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("newBrowserHTTP short timeout: %v", err)
	}
	short.CloseIdleConnections()
}

func TestMustJSONAndRandomUUID(t *testing.T) {
	if got := mustJSON(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Fatalf("mustJSON = %q", got)
	}
	first := randomUUID()
	if len(first) != 36 || strings.Count(first, "-") != 4 {
		t.Fatalf("randomUUID shape = %q", first)
	}
	if first == randomUUID() {
		t.Fatal("randomUUID should not repeat")
	}
}

func TestImageIDFromURL(t *testing.T) {
	if got := imageIDFromURL("https://cdn.test/media/abc-123.png"); got != "abc-123" {
		t.Fatalf("imageIDFromURL = %q", got)
	}
	if got := imageIDFromURL("https://cdn.test/media/abc-123"); got != "abc-123" {
		t.Fatalf("extensionless = %q", got)
	}
	generated := imageIDFromURL("")
	if len(generated) != 36 {
		t.Fatalf("empty url should generate uuid, got %q", generated)
	}
}

func TestBoolValue(t *testing.T) {
	if !boolValue(true) {
		t.Fatal("true should convert")
	}
	if boolValue("true") || boolValue(nil) {
		t.Fatal("non-bool should be false")
	}
}

func TestGrokHeaders(t *testing.T) {
	derived := headers(accounts.Account{Token: "sso=abc"})
	if derived["Cookie"] != "sso=abc; sso-rw=abc" {
		t.Fatalf("derived cookie = %q", derived["Cookie"])
	}
	custom := headers(accounts.Account{Token: "tok", Fields: map[string]any{"cookie_header": "custom=1"}})
	if custom["Cookie"] != "custom=1" {
		t.Fatalf("custom cookie = %q", custom["Cookie"])
	}
	if custom["Origin"] != "https://grok.com" {
		t.Fatalf("origin = %q", custom["Origin"])
	}
}

func TestReadError(t *testing.T) {
	response := &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limited"))}
	err := ReadError(response)
	if err.Status != 429 || !strings.Contains(err.Message, "rate limited") {
		t.Fatalf("ReadError = %+v", err)
	}

	empty := &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}
	fallback := ReadError(empty)
	if !strings.Contains(fallback.Message, "500") {
		t.Fatalf("expected status fallback, got %q", fallback.Message)
	}
}

func TestReadConsoleError(t *testing.T) {
	response := &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("console said no"))}
	err := ReadConsoleError(response)
	if err.Status != 401 || !strings.Contains(err.Message, "console said no") {
		t.Fatalf("ReadConsoleError = %+v", err)
	}

	empty := &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader(""))}
	if !strings.Contains(ReadConsoleError(empty).Message, "502") {
		t.Fatal("expected status fallback")
	}
}

func TestConsoleTimeout(t *testing.T) {
	if got := ConsoleTimeout(0); got != 120*time.Second {
		t.Fatalf("zero timeout = %v", got)
	}
	if got := ConsoleTimeout(-time.Second); got != 120*time.Second {
		t.Fatalf("negative timeout = %v", got)
	}
	if got := ConsoleTimeout(30 * time.Second); got != 30*time.Second {
		t.Fatalf("explicit timeout = %v", got)
	}
}

func TestMediaID(t *testing.T) {
	first := mediaID("seed")
	if len(first) != 32 {
		t.Fatalf("mediaID length = %d (%q)", len(first), first)
	}
}
