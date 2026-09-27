package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/auucoder/gptgrok2api-go/internal/accounts"
)

// editableServer mimics the ChatGPT web backend for the editable export flow:
// bootstrap, sentinel requirements, conduit prepare, SSE start, artifact poll and
// artifact download.
type editableServerOptions struct {
	conversationBody string
	streamBody       string
	prepareResponse  map[string]any
}

func newEditableServer(t *testing.T, options editableServerOptions) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<html data-build="test-build"><script src="/static/app.js"></script></html>`))
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"prepare_token": "prepare-token"})
		case "/backend-api/sentinel/chat-requirements/finalize":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "requirements-token"})
		case "/backend-api/f/conversation/prepare":
			if options.prepareResponse != nil {
				_ = json.NewEncoder(w).Encode(options.prepareResponse)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"conduit_token": "conduit-token"})
		case "/backend-api/f/conversation":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(options.streamBody))
		default:
			if r.URL.Path == "/backend-api/conversation/editable-conversation" {
				_, _ = w.Write([]byte(options.conversationBody))
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestExportEditableRejectsUnknownKind(t *testing.T) {
	client := NewOpenAIImage("https://example.invalid", nil, nil, time.Second)
	_, err := client.ExportEditable(context.Background(), accounts.Account{}, "PDF", "prompt", nil)
	if err == nil || !strings.Contains(err.Error(), "must be ppt or psd") {
		t.Fatalf("expected kind validation error, got %v", err)
	}
}

func TestExportEditablePSDRequiresInput(t *testing.T) {
	client := NewOpenAIImage("https://example.invalid", nil, nil, time.Second)
	_, err := client.ExportEditable(context.Background(), accounts.Account{}, "psd", "prompt", nil)
	if err == nil || !strings.Contains(err.Error(), "at least one image") {
		t.Fatalf("expected psd input validation error, got %v", err)
	}
}

func TestExportEditableFullFlow(t *testing.T) {
	attachmentID := "01JSEDITABLE1234567890"
	server := newEditableServer(t, editableServerOptions{
		streamBody: "data: {\"conversation_id\":\"editable-conversation\"}\n\ndata: [DONE]\n\n",
		conversationBody: `{"mapping":{"assistant":{"id":"m1","author":{"role":"assistant"},"attachments":[` +
			`{"attachment_id":"` + attachmentID + `","name":"deck.pptx","mime_type":"application/vnd.ms-powerpoint"},` +
			`{"attachment_id":"` + attachmentID + `z","name":"assets.zip","mime_type":"application/zip"}]}}}`,
	})
	// Download routes for both artifacts.
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/backend-api/conversation/editable-conversation/attachment/") {
			switch {
			case strings.HasSuffix(r.URL.Path, "/"+attachmentID+"/download"):
				w.Header().Set("Content-Type", "application/vnd.ms-powerpoint")
				_, _ = w.Write([]byte("pptx-bytes"))
			case strings.HasSuffix(r.URL.Path, "/"+attachmentID+"z/download"):
				w.Header().Set("Content-Type", "application/zip")
				_, _ = w.Write([]byte("zip-bytes"))
			default:
				http.NotFound(w, r)
			}
			return
		}
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<html data-build="test-build"><script src="/static/app.js"></script></html>`))
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"prepare_token": "prepare-token"})
		case "/backend-api/sentinel/chat-requirements/finalize":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "requirements-token"})
		case "/backend-api/f/conversation/prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"conduit_token": "conduit-token"})
		case "/backend-api/f/conversation":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"conversation_id\":\"editable-conversation\"}\n\ndata: [DONE]\n\n"))
		case "/backend-api/conversation/editable-conversation":
			_, _ = w.Write([]byte(`{"mapping":{"assistant":{"id":"m1","author":{"role":"assistant"},"attachments":[` +
				`{"attachment_id":"` + attachmentID + `","name":"deck.pptx","mime_type":"application/vnd.ms-powerpoint"},` +
				`{"attachment_id":"` + attachmentID + `z","name":"assets.zip","mime_type":"application/zip"}]}}}`))
		default:
			http.NotFound(w, r)
		}
	})

	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	result, err := client.ExportEditable(context.Background(), accounts.Account{Token: "jwt.header.payload"}, "ppt", "强调品牌色", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ConversationID != "editable-conversation" {
		t.Fatalf("conversation id = %q", result.ConversationID)
	}
	if result.Primary.Name != "deck.pptx" || string(result.Primary.Data) != "pptx-bytes" {
		t.Fatalf("unexpected primary file: %+v", result.Primary)
	}
	if result.Archive.Name != "assets.zip" || string(result.Archive.Data) != "zip-bytes" {
		t.Fatalf("unexpected archive file: %+v", result.Archive)
	}
}

func TestExportEditableFailsWhenPrepareHasNoConduit(t *testing.T) {
	server := newEditableServer(t, editableServerOptions{prepareResponse: map[string]any{"conduit_token": ""}})
	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	_, err := client.ExportEditable(context.Background(), accounts.Account{Token: "jwt.header.payload"}, "ppt", "", nil)
	if err == nil || !strings.Contains(err.Error(), "no conduit token") {
		t.Fatalf("expected conduit error, got %v", err)
	}
}

func TestExportEditableFailsWhenStreamHasNoConversationID(t *testing.T) {
	server := newEditableServer(t, editableServerOptions{streamBody: "data: [DONE]\n\n"})
	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	_, err := client.ExportEditable(context.Background(), accounts.Account{Token: "jwt.header.payload"}, "ppt", "", nil)
	if err == nil || !strings.Contains(err.Error(), "no conversation id") {
		t.Fatalf("expected conversation id error, got %v", err)
	}
}

func TestExportEditableFailsWhenArtifactsAreMissing(t *testing.T) {
	server := newEditableServer(t, editableServerOptions{
		streamBody:       "data: {\"conversation_id\":\"editable-conversation\"}\n\n",
		conversationBody: `{"mapping":{"assistant":{"id":"m1","author":{"role":"assistant"},"text":"nothing yet"}}}`,
	})
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<html data-build="test-build"><script src="/static/app.js"></script></html>`))
		case "/backend-api/sentinel/chat-requirements/prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"prepare_token": "prepare-token"})
		case "/backend-api/sentinel/chat-requirements/finalize":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "requirements-token"})
		case "/backend-api/f/conversation/prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"conduit_token": "conduit-token"})
		case "/backend-api/f/conversation":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"conversation_id\":\"editable-conversation\"}\n\n"))
		case "/backend-api/conversation/editable-conversation":
			_, _ = w.Write([]byte(`{"mapping":{"assistant":{"id":"m1","author":{"role":"assistant"},"text":"still working"}}}`))
		default:
			http.NotFound(w, r)
		}
	})
	client := NewOpenAIImage(server.URL, server.Client(), nil, 10*time.Second)
	// pollEditableArtifacts clamps its timeout to a 20 minute floor, so the only
	// way to get a context error back is to cancel the context.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := client.ExportEditable(ctx, accounts.Account{Token: "jwt.header.payload"}, "ppt", "", nil)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestPrepareEditableSendsAttachmentMIMEs(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/f/conversation/prepare" {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			if got := r.Header.Get("X-Conduit-Token"); got != "no-token" {
				t.Errorf("conduit placeholder header = %q", got)
			}
		}
		_, _ = w.Write([]byte(`{"conduit_token":"conduit-token"}`))
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	conduit, err := client.prepareEditable(context.Background(), accounts.Account{}, openAIRequirements{}, "prompt", []openAIImageReference{{MIME: "image/png"}})
	if err != nil {
		t.Fatal(err)
	}
	if conduit != "conduit-token" {
		t.Fatalf("conduit = %q", conduit)
	}
	types, _ := captured["attachment_mime_types"].([]any)
	if len(types) != 1 || types[0] != "image/png" {
		t.Fatalf("attachment mime types = %#v", captured["attachment_mime_types"])
	}
	if captured["model"] != EditableFileModel {
		t.Fatalf("model = %v", captured["model"])
	}
}

func TestStartEditableBuildsMultimodalParts(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/f/conversation" {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			if got := r.Header.Get("X-Conduit-Token"); got != "conduit-token" {
				t.Errorf("conduit header = %q", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: message\ndata: {\"conversation_id\":\"conv-x\"}\n\ndata: [DONE]\n\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	id, err := client.startEditable(context.Background(), accounts.Account{}, openAIRequirements{}, "conduit-token", "prompt", []openAIImageReference{
		{FileID: "file-1", FileName: "a.png", FileSize: 10, MIME: "image/png", Width: 4, Height: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "conv-x" {
		t.Fatalf("conversation id = %q", id)
	}
	message := captured["messages"].([]any)[0].(map[string]any)
	content := message["content"].(map[string]any)
	if content["content_type"] != "multimodal_text" {
		t.Fatalf("content type = %v", content["content_type"])
	}
	parts := content["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("expected pointer plus prompt, got %#v", parts)
	}
	pointer := parts[0].(map[string]any)
	if pointer["asset_pointer"] != "sediment://file-1" {
		t.Fatalf("asset pointer = %v", pointer["asset_pointer"])
	}
	metadata := message["metadata"].(map[string]any)
	attachments, _ := metadata["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("expected one attachment, got %#v", metadata["attachments"])
	}
}

func TestStartEditableWithoutReferencesUsesTextContent(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = fmt.Fprint(w, "data: {\"conversation_id\":\"conv-y\"}\n\n")
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	if _, err := client.startEditable(context.Background(), accounts.Account{}, openAIRequirements{}, "c", "prompt", nil); err != nil {
		t.Fatal(err)
	}
	message := captured["messages"].([]any)[0].(map[string]any)
	if message["content"].(map[string]any)["content_type"] != "text" {
		t.Fatalf("expected text content, got %#v", message["content"])
	}
	if _, present := message["metadata"].(map[string]any)["attachments"]; present {
		t.Fatal("metadata should not carry attachments without references")
	}
}

func TestPollEditableArtifactsStopsOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"mapping":{}}`))
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := client.pollEditableArtifacts(ctx, accounts.Account{}, "editable-conversation", "ppt")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestDownloadEditableArtifactPrefersSandboxInterpreter(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		if strings.Contains(r.URL.Path, "/interpreter/download") {
			if r.URL.Query().Get("sandbox_path") != "/mnt/data/deck.pptx" || r.URL.Query().Get("message_id") != "m1" {
				t.Errorf("unexpected sandbox query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/vnd.ms-powerpoint")
			_, _ = w.Write([]byte("sandbox-pptx"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	file, err := client.downloadEditableArtifact(context.Background(), accounts.Account{}, "editable-conversation", editableArtifact{
		SandboxPath: "/mnt/data/deck.pptx", MessageID: "m1", AttachmentID: "att-1",
	}, "ppt")
	if err != nil {
		t.Fatal(err)
	}
	if string(file.Data) != "sandbox-pptx" || file.Name != "deck.pptx" {
		t.Fatalf("unexpected file: %+v", file)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "/interpreter/download") {
		t.Fatalf("expected single sandbox attempt, got %#v", paths)
	}
}

func TestDownloadEditableArtifactFollowsFilesDownloadRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/files/file-1/download":
			_ = json.NewEncoder(w).Encode(map[string]any{"download_url": "http://" + r.Host + "/blob/deck"})
		case "/blob/deck":
			w.Header().Set("Content-Disposition", `attachment; filename="renamed.pptx"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("redirected-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	file, err := client.downloadEditableArtifact(context.Background(), accounts.Account{}, "editable-conversation", editableArtifact{FileID: "file-1"}, "ppt")
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != "renamed.pptx" {
		t.Fatalf("name from content disposition = %q", file.Name)
	}
	if string(file.Data) != "redirected-bytes" {
		t.Fatalf("data = %q", file.Data)
	}
}

func TestDownloadEditableArtifactFallsBackToAttachmentRoute(t *testing.T) {
	var routes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/attachment/att-9/download") {
			routes = append(routes, r.Header.Get("X-OpenAI-Target-Route"))
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("zip-bytes"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	file, err := client.downloadEditableArtifact(context.Background(), accounts.Account{}, "editable-conversation", editableArtifact{AttachmentID: "att-9"}, "zip")
	if err != nil {
		t.Fatal(err)
	}
	if string(file.Data) != "zip-bytes" {
		t.Fatalf("data = %q", file.Data)
	}
	if len(routes) != 1 || routes[0] != "/backend-api/conversation/{conversation_id}/attachment/{attachment_id}/download" {
		t.Fatalf("unexpected target routes: %#v", routes)
	}
}

func TestDownloadEditableArtifactFailsWhenNoRouteWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	_, err := client.downloadEditableArtifact(context.Background(), accounts.Account{}, "editable-conversation", editableArtifact{}, "ppt")
	if err == nil || !strings.Contains(err.Error(), "download editable artifact") {
		t.Fatalf("expected download failure, got %v", err)
	}
}

func TestEditableResponseFileRejectsEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	response, err := client.Client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.editableResponseFile(context.Background(), accounts.Account{}, response, editableArtifact{}, "ppt")
	if err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("expected empty content error, got %v", err)
	}
}

func TestEditableResponseFileNamesFromFinalURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("bytes"))
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	response, err := client.Client.Get(server.URL + "/files/layers.psd")
	if err != nil {
		t.Fatal(err)
	}
	file, err := client.editableResponseFile(context.Background(), accounts.Account{}, response, editableArtifact{}, "psd")
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != "layers.psd" {
		t.Fatalf("name derived from URL = %q", file.Name)
	}
}

func TestEditableResponseFileUsesFallbackArtifactName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("bytes"))
	}))
	defer server.Close()
	client := NewOpenAIImage(server.URL, server.Client(), nil, 5*time.Second)
	response, err := client.Client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	file, err := client.editableResponseFile(context.Background(), accounts.Account{}, response, editableArtifact{}, "zip")
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != "artifact.zip" {
		t.Fatalf("fallback name = %q", file.Name)
	}
}

func TestCollectEditableArtifactsMergesSplitPointers(t *testing.T) {
	items := collectEditableArtifacts(map[string]any{
		"mapping": map[string]any{
			"a": map[string]any{"message": map[string]any{
				"id": "m1", "author": map[string]any{"role": "assistant"},
				"content": map[string]any{"parts": []any{
					map[string]any{"content_type": "image_asset_pointer", "asset_pointer": "file-service://file-abc"},
				}},
			}},
			"b": map[string]any{"sandbox_path": "/mnt/data/output/deck.pptx", "file_id": "file-abc"},
		},
	})
	var merged *editableArtifact
	for index := range items {
		if items[index].FileID == "file-abc" {
			merged = &items[index]
		}
	}
	if merged == nil {
		t.Fatalf("file-abc artifact not collected: %+v", items)
	}
	if merged.AttachmentID != "file-abc" {
		t.Fatalf("attachment id = %q", merged.AttachmentID)
	}
	if merged.SandboxPath != "/mnt/data/output/deck.pptx" {
		t.Fatalf("sandbox path = %q", merged.SandboxPath)
	}
	if merged.MessageID != "m1" {
		t.Fatalf("message id = %q", merged.MessageID)
	}
}

func TestCollectEditableArtifactsUsesFileNameAliases(t *testing.T) {
	// A node is only kept when it carries an id or sandbox path, so each case
	// pairs an alias with a distinct file id to force a separate artifact.
	items := collectEditableArtifacts([]any{
		map[string]any{"file_id": "file-1", "file_name": "first.pptx", "mimeType": "application/vnd.ms-powerpoint; charset=utf-8"},
		map[string]any{"file_id": "file-2", "filename": "second.psd"},
		map[string]any{"file_id": "file-3", "title": "third.zip"},
		map[string]any{"file_id": "file-4"},
	})
	byID := map[string]editableArtifact{}
	for _, item := range items {
		byID[item.FileID] = item
	}
	first, ok := byID["file-1"]
	if !ok || first.Name != "first.pptx" {
		t.Fatalf("file_name alias not parsed: %+v", items)
	}
	if first.MIME != "application/vnd.ms-powerpoint" {
		t.Fatalf("mimeType alias not normalized: %q", first.MIME)
	}
	if byID["file-2"].Name != "second.psd" {
		t.Fatalf("filename alias not parsed: %+v", items)
	}
	if byID["file-3"].Name != "third.zip" {
		t.Fatalf("title alias not parsed: %+v", items)
	}
	if byID["file-4"].Name != "" {
		t.Fatalf("expected no name without aliases, got %+v", byID["file-4"])
	}
}

func TestCollectEditableArtifactsFindsSandboxPathInText(t *testing.T) {
	// Only the first regex match survives per node, so each path needs its own
	// message node to show up as a distinct artifact.
	items := collectEditableArtifacts(map[string]any{
		"mapping": map[string]any{
			"a": map[string]any{"message": map[string]any{
				"id": "m-42", "author": map[string]any{"role": "assistant"},
				"content": map[string]any{"content_type": "text", "parts": []any{
					`Done, the deck is ready at /mnt/data/output/report.pptx`,
				}},
			}},
			"b": map[string]any{"message": map[string]any{
				"id": "m-43", "author": map[string]any{"role": "assistant"},
				"content": map[string]any{"content_type": "text", "parts": []any{
					`Assets are bundled at /mnt/data/output/assets.zip`,
				}},
			}},
		},
	})
	paths := map[string]string{}
	for _, item := range items {
		paths[item.SandboxPath] = item.MessageID
	}
	if paths["/mnt/data/output/report.pptx"] != "m-42" {
		t.Fatalf("report.pptx not attributed to m-42: %#v", items)
	}
	if paths["/mnt/data/output/assets.zip"] != "m-43" {
		t.Fatalf("assets.zip not attributed to m-43: %#v", items)
	}
}

func TestPickEditableArtifactsMatchesMIMEOnly(t *testing.T) {
	items := []editableArtifact{
		{FileID: "p", MIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{FileID: "z", MIME: "application/x-zip-compressed"},
		{FileID: "s", MIME: "image/vnd.adobe.photoshop"},
	}
	primary, archive := pickEditableArtifacts(items, "ppt")
	if primary == nil || primary.FileID != "p" {
		t.Fatalf("ppt primary = %+v", primary)
	}
	if archive == nil || archive.FileID != "z" {
		t.Fatalf("x-zip archive = %+v", archive)
	}
	psdPrimary, _ := pickEditableArtifacts(items, "psd")
	if psdPrimary == nil || psdPrimary.FileID != "s" {
		t.Fatalf("photoshop primary = %+v", psdPrimary)
	}
}
