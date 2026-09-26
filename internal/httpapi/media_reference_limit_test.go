package httpapi

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auucoder/gptgrok2api-go/internal/config"
)

func writeReferenceLimitConfig(root string, content string) error {
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	configPath := filepath.Join(root, "config.json")
	if content == "" {
		content = "{}"
	}
	return os.WriteFile(configPath, []byte(content), 0o600)
}

func newReferenceLimitServer(t *testing.T, maxReferences int) *Server {
	t.Helper()
	root := t.TempDir()
	cfg := testConfig()
	cfg.RootDir = root
	cfg.DataDir = filepath.Join(root, "data")
	cfg.ConfigPath = filepath.Join(root, "config.json")
	cfg.AccountsPath = filepath.Join(root, "data", "accounts.json")
	cfg.AuthKeysPath = filepath.Join(root, "data", "auth_keys.json")
	cfg.ImageDataDir = filepath.Join(root, "images")
	cfg.ImageMaxReferences = maxReferences
	return New(cfg)
}

// postImageEditJSON sends an image edit built from count identical data URLs so
// the reference cap is the only variable between the cases below.
func postImageEditJSON(t *testing.T, server *Server, count int) *httptest.ResponseRecorder {
	t.Helper()
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG)
	escapedURL := strings.ReplaceAll(dataURL, `"`, `\"`)
	urls := make([]string, 0, count)
	for range count {
		urls = append(urls, `"`+escapedURL+`"`)
	}
	body := fmt.Sprintf(`{"model":"grok-imagine-image-edit","prompt":"换成夜景","image_url":[%s]}`,
		strings.Join(urls, ","))
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+server.cfg.APIKey)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func TestImageEditReferenceLimitRejectsExcessImages(t *testing.T) {
	server := newReferenceLimitServer(t, 3)

	response := postImageEditJSON(t, server, 4)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 4 references with a limit of 3, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "too many reference images") {
		t.Fatalf("expected a reference limit diagnostic, got %s", body)
	}
	if !strings.Contains(body, "got 4") || !strings.Contains(body, "maximum allowed is 3") {
		t.Fatalf("expected the error to report the sent count and the cap, got %s", body)
	}
}

func TestImageEditReferenceLimitAcceptsExactCap(t *testing.T) {
	server := newReferenceLimitServer(t, 3)

	response := postImageEditJSON(t, server, 3)

	// The cap check must let the request continue into upstream work, so the
	// failure here can only come from the missing account pool.
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the request to pass the limit check, got %d: %s", response.Code, response.Body.String())
	}
}

func TestImageEditReferenceLimitRespectsConfigValue(t *testing.T) {
	// A single image must be rejected once the cap drops below it.
	server := newReferenceLimitServer(t, 0)

	response := postImageEditJSON(t, server, 8)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "too many reference images") {
		t.Fatalf("expected the unconfigured default cap of 7 to reject 8 references, got %d: %s", response.Code, response.Body.String())
	}
}

func TestConfigImageMaxReferencesDefaultsToSeven(t *testing.T) {
	t.Setenv("GO_IMAGE_MAX_REFERENCES", "")
	root := t.TempDir()
	if err := writeReferenceLimitConfig(root, ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageMaxReferences != 7 {
		t.Fatalf("expected a default cap of 7, got %d", cfg.ImageMaxReferences)
	}
}

func TestConfigImageMaxReadsEnvironmentAndFile(t *testing.T) {
	root := t.TempDir()
	if err := writeReferenceLimitConfig(root, `{"image_max_references": 5}`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_IMAGE_MAX_REFERENCES", "")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageMaxReferences != 5 {
		t.Fatalf("expected config.json to set the cap to 5, got %d", cfg.ImageMaxReferences)
	}

	// An explicit environment value stays authoritative for containers.
	t.Setenv("GO_IMAGE_MAX_REFERENCES", "9")
	cfg, err = config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageMaxReferences != 9 {
		t.Fatalf("expected the environment to win with 9, got %d", cfg.ImageMaxReferences)
	}
}

func TestConfigImageMaxReferencesClampsOutOfRange(t *testing.T) {
	root := t.TempDir()
	if err := writeReferenceLimitConfig(root, `{"image_max_references": 5000}`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_IMAGE_MAX_REFERENCES", "")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageMaxReferences != 7 {
		t.Fatalf("expected an out-of-range config value to fall back to 7, got %d", cfg.ImageMaxReferences)
	}
}
