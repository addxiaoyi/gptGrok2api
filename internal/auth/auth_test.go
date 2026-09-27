package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/auucoder/gptgrok2api-go/internal/store"
)

func TestValidatorAcceptsBearerAndAPIKey(t *testing.T) {
	validator := New("api-secret", "admin-secret", "", false, nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer api-secret")
	if !validator.ValidAPIRequest(request) {
		t.Fatal("expected bearer API key to be accepted")
	}
	request.Header.Set("X-API-Key", "admin-secret")
	request.Header.Del("Authorization")
	if !validator.ValidAdminRequest(request) {
		t.Fatal("expected admin API key to be accepted")
	}
}

func TestLegacyAPIKeyIsAdminWithoutSeparateAdminKey(t *testing.T) {
	validator := New("legacy-secret", "", "", false, nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer legacy-secret")
	if !validator.ValidAdminRequest(request) {
		t.Fatal("expected legacy API key to retain admin access")
	}
	identity, ok := validator.Identity("legacy-secret")
	if !ok || identity.Role != "admin" {
		t.Fatalf("expected admin identity, got %#v", identity)
	}
}

func TestSeparateAPIKeyDoesNotAuthorizeAdminRoutes(t *testing.T) {
	validator := New("api-secret", "admin-secret", "", false, nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer api-secret")
	if validator.ValidAdminRequest(request) {
		t.Fatal("expected separate API key to be rejected for admin access")
	}
	identity, ok := validator.Identity("api-secret")
	if !ok || identity.Role != "user" {
		t.Fatalf("expected user identity, got %#v", identity)
	}
}

func TestHasConfiguredKey(t *testing.T) {
	if New("", "", "", false, nil).HasConfiguredKey() {
		t.Error("expected HasConfiguredKey=false when nothing configured")
	}
	if !New("k", "", "", false, nil).HasConfiguredKey() {
		t.Error("expected HasConfiguredKey=true when apiKey set")
	}
	if !New("", "k", "", false, nil).HasConfiguredKey() {
		t.Error("expected HasConfiguredKey=true when adminKey set")
	}
	if !New("", "", "/path/to/keys.json", false, nil).HasConfiguredKey() {
		t.Error("expected HasConfiguredKey=true when authKeysPath set")
	}
}

func TestMatches(t *testing.T) {
	v := New("secret", "", "", false, nil)
	if v.matches("secret", "secret") != true {
		t.Error("expected match")
	}
	if v.matches("secret", "other") != false {
		t.Error("expected mismatch")
	}
	if v.matches("", "secret") != false {
		t.Error("expected empty candidate to reject")
	}
	if v.matches("secret", "") != false {
		t.Error("expected empty expected to reject")
	}
	if v.matches("", "") != false {
		t.Error("expected both empty to reject")
	}
}

func TestMatchesStoredHashRejectsMissingFile(t *testing.T) {
	v := New("", "", t.TempDir()+"/does_not_exist.json", false, nil)
	if v.matchesStoredHash("any-token") {
		t.Error("expected missing auth_keys file to reject")
	}
}

func TestMatchesStoredHashRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	if err := os.WriteFile(path, []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New("", "", path, false, nil)
	if v.matchesStoredHash("any-token") {
		t.Error("expected malformed JSON to reject")
	}
}

func TestMatchesStoredHashAcceptsKeyHash(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	raw := `{"items":[{"key_hash":"` + hashSHA256("token-abc") + `","enabled":true,"name":"test"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New("", "", path, false, nil)
	if !v.matchesStoredHash("token-abc") {
		t.Error("expected stored key_hash to authenticate token")
	}
	if v.matchesStoredHash("wrong-token") {
		t.Error("expected wrong token to be rejected")
	}
}

func TestMatchesStoredHashAcceptsPlaintextKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	raw := `{"items":[{"key":"plain-key-xyz","enabled":true}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New("", "", path, false, nil)
	if !v.matchesStoredHash("plain-key-xyz") {
		t.Error("expected stored key field to authenticate token")
	}
}

func TestMatchesStoredHashSkipsDisabledItems(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	raw := `{"items":[{"key":"disabled-key","enabled":false},{"key":"active-key","enabled":true}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New("", "", path, false, nil)
	if v.matchesStoredHash("disabled-key") {
		t.Error("expected disabled key to be rejected")
	}
	if !v.matchesStoredHash("active-key") {
		t.Error("expected active key to authenticate")
	}
}

func TestMatchesStoredHashIgnoresMissingEnabledField(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	raw := `{"items":[{"key":"no-enabled-field"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New("", "", path, false, nil)
	if !v.matchesStoredHash("no-enabled-field") {
		t.Error("expected missing enabled field to default to true")
	}
}

func TestExtractItemsFromArray(t *testing.T) {
	items := extractItems([]any{map[string]any{"a": 1}, "non-object", map[string]any{"b": 2}})
	if len(items) != 2 {
		t.Fatalf("expected 2 valid items, got %d", len(items))
	}
	if items[0]["a"] != 1 || items[1]["b"] != 2 {
		t.Fatal("items mismatch")
	}
}

func TestExtractItemsFromNestedMap(t *testing.T) {
	inner := map[string]any{"items": []any{map[string]any{"id": "x"}}}
	items := extractItems(inner)
	if len(items) != 1 {
		t.Fatalf("expected 1 item from nested map, got %d", len(items))
	}
	if items[0]["id"] != "x" {
		t.Fatal("wrong item content")
	}
}

func TestExtractItemsFromEmptyMapReturnsNil(t *testing.T) {
	if extractItems(map[string]any{}) != nil {
		t.Fatal("expected nil for map without items key")
	}
}

func TestExtractItemsFromUnknownTypeReturnsNil(t *testing.T) {
	if extractItems("not-structured") != nil {
		t.Fatal("expected nil for unknown type")
	}
}

func TestValidAPIRequestUsesStoredHash(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	raw := `{"items":[{"key":"stored-token","enabled":true}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New("", "", path, false, nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer stored-token")
	if !v.ValidAPIRequest(request) {
		t.Error("expected ValidAPIRequest to accept stored hash token")
	}
}

func TestValidAdminRequestQueryOnlyWhenAllowed(t *testing.T) {
	v := NewWithOptions("", "admin-q", "", false, nil, Options{AllowQueryAdmin: true})
	request, _ := http.NewRequest(http.MethodGet, "/?app_key=admin-q", nil)
	if !v.ValidAdminRequest(request) {
		t.Error("expected ValidAdminRequest to accept app_key query when allowQueryAdmin=true")
	}

	vDisallowed := NewWithOptions("", "admin-q", "", false, nil, Options{})
	if vDisallowed.ValidAdminRequest(request) {
		t.Error("expected ValidAdminRequest to reject app_key query when allowQueryAdmin=false")
	}
}

func TestIdentityUnknownToken(t *testing.T) {
	v := New("a", "b", "", false, nil)
	if _, ok := v.Identity("unknown"); ok {
		t.Error("expected unknown token to be rejected")
	}
	if _, ok := v.Identity("   "); ok {
		t.Error("expected blank token to be rejected")
	}
}

func TestIdentityMatchesStoredHash(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth_keys.json"
	raw := `{"items":[{"key":"ident-token","enabled":true}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	// repository 为 nil 时不会回落到 matchesStoredHash，凭据只用于 ValidAPIRequest。
	v := New("", "", path, false, nil)
	if _, ok := v.Identity("ident-token"); ok {
		t.Error("expected Identity to reject stored-hash token when no repository configured")
	}
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer ident-token")
	if !v.ValidAPIRequest(request) {
		t.Error("expected ValidAPIRequest to accept stored-hash token")
	}
}

func TestAdminKeyReturnsHeaderOverQuery(t *testing.T) {
	v := NewWithOptions("api-k", "admin-k", "", false, nil, Options{AllowQueryAdmin: true})
	request, _ := http.NewRequest(http.MethodGet, "/?app_key=query-k", nil)
	request.Header.Set("Authorization", "Bearer admin-k")
	if got := v.AdminKey(request); got != "admin-k" {
		t.Errorf("expected header to take precedence, got %q", got)
	}
}

func TestAdminKeyEmptyWhenDisallowedAndNoHeader(t *testing.T) {
	v := NewWithOptions("api-k", "admin-k", "", false, nil, Options{})
	request, _ := http.NewRequest(http.MethodGet, "/?app_key=query-k", nil)
	if got := v.AdminKey(request); got != "" {
		t.Errorf("expected empty when query admin disallowed, got %q", got)
	}
}

func TestAPIKeyExtractsBearerAndXKey(t *testing.T) {
	v := New("", "", "", false, nil)

	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer  spaced ")
	if got := v.APIKey(request); got != "spaced" {
		t.Errorf("expected bearer token trimmed, got %q", got)
	}

	request.Header.Set("X-API-Key", " raw-x ")
	request.Header.Del("Authorization")
	if got := v.APIKey(request); got != "raw-x" {
		t.Errorf("expected X-API-Key trimmed, got %q", got)
	}

	request.Header.Set("Authorization", "Basic abc")
	request.Header.Del("X-API-Key")
	if got := v.APIKey(request); got != "" {
		t.Errorf("expected empty for non-bearer scheme, got %q", got)
	}
}

func TestValidAPIRequestAnonymous(t *testing.T) {
	v := New("", "", "", true, nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	if !v.ValidAPIRequest(request) {
		t.Error("expected ValidAPIRequest=true when allowAnonymous=true and no token")
	}
	v2 := New("", "", "", false, nil)
	if v2.ValidAPIRequest(request) {
		t.Error("expected ValidAPIRequest=false when allowAnonymous=false and no token")
	}
}

func TestValidAPIRequestWithRepository(t *testing.T) {
	root := t.TempDir()
	repo := store.New(
		filepath.Join(root, "accounts.json"),
		filepath.Join(root, "auth_keys.json"),
		filepath.Join(root, "config.json"),
	)
	_, rawKey, err := repo.CreateKey("admin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	v := New("", "", "", false, repo)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+rawKey)
	if !v.ValidAPIRequest(request) {
		t.Error("expected ValidAPIRequest to accept repository key")
	}
	if !v.ValidAdminRequest(request) {
		t.Error("expected ValidAdminRequest to accept admin repo key")
	}
}

func TestValidAdminRequestWithRepositoryRejectsUser(t *testing.T) {
	root := t.TempDir()
	repo := store.New(
		filepath.Join(root, "accounts.json"),
		filepath.Join(root, "auth_keys.json"),
		filepath.Join(root, "config.json"),
	)
	_, userKey, err := repo.CreateKey("user", "", "")
	if err != nil {
		t.Fatal(err)
	}
	v := New("", "", "", false, repo)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+userKey)
	if v.ValidAdminRequest(request) {
		t.Error("expected user key to be rejected for admin routes")
	}
}

func TestAdminKeyEmptyNoToken(t *testing.T) {
	v := New("", "", "", false, nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	if got := v.AdminKey(request); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestIdentityReturnsAdminForAdminKey(t *testing.T) {
	v := New("", "admin-token", "", false, nil)
	identity, ok := v.Identity("admin-token")
	if !ok || identity.Role != "admin" {
		t.Fatalf("expected admin identity, got %#v", identity)
	}
}

func TestIdentityReturnsAPIForSeparateKey(t *testing.T) {
	v := New("api-token", "admin-token", "", false, nil)
	identity, ok := v.Identity("api-token")
	if !ok || identity.Role != "user" {
		t.Fatalf("expected user identity, got %#v", identity)
	}
}

func TestIdentityReturnsAdminWhenAPIKeyMatchesAdminKey(t *testing.T) {
	v := New("same-token", "same-token", "", false, nil)
	identity, ok := v.Identity("same-token")
	if !ok || identity.Role != "admin" {
		t.Fatalf("expected admin identity when apiKey == adminKey, got %#v", identity)
	}
}

func hashSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
