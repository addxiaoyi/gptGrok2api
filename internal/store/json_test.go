package store

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthKeyLifecycleKeepsPrivateHashOutOfPublicPayload(t *testing.T) {
	root := t.TempDir()
	repository := New(
		filepath.Join(root, "accounts.json"),
		filepath.Join(root, "auth_keys.json"),
		filepath.Join(root, "config.json"),
	)

	item, rawKey, err := repository.CreateKey("user", "", "admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	if rawKey == "" || item["key_hash"] != nil {
		t.Fatalf("public key payload leaked private material: %#v", item)
	}

	identity, ok := repository.Authenticate(rawKey)
	if !ok || identity.Role != "user" {
		t.Fatalf("created key did not authenticate: %#v", identity)
	}
}

func TestAddAccountsWithinBatchDeduplicatesOnToken(t *testing.T) {
	root := t.TempDir()
	repository := New(filepath.Join(root, "accounts.json"), filepath.Join(root, "auth_keys.json"), filepath.Join(root, "config.json"))

	// 同一 token 同时出现在 payloads 与 tokens 里，batch 内只计一次。
	added, skipped, items, err := repository.AddAccounts(
		[]string{"dup-token", "new-token"},
		[]map[string]any{
			{"access_token": "dup-token", "email": "dup@example.test"},
			{"accessToken": "alt-key-token"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("expected one in-batch skip, got %d", skipped)
	}
	// payloads 的 2 个 + tokens 里新增的 1 个 = 3 个账号
	if added != 3 || len(items) != 3 {
		t.Fatalf("expected 3 stored accounts, added=%d items=%#v", added, items)
	}

	// 第二批命中已存在的 token，仍应去重。
	added, skipped, items, err = repository.AddAccounts(
		[]string{"dup-token"},
		[]map[string]any{{"access_token": "dup-token"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || skipped != 2 {
		t.Fatalf("expected no adds and 2 skips, got added=%d skipped=%d", added, skipped)
	}
	if len(items) != 3 {
		t.Fatalf("stored count drifted: %d", len(items))
	}
}

func TestAddAccountsNormalizesAltKeyTokenToCanonical(t *testing.T) {
	root := t.TempDir()
	accountPath := filepath.Join(root, "accounts.json")
	repository := New(accountPath, filepath.Join(root, "auth_keys.json"), filepath.Join(root, "config.json"))

	_, _, _, err := repository.AddAccounts(nil, []map[string]any{{"accessToken": "alt-token"}})
	if err != nil {
		t.Fatal(err)
	}
	// accessToken 应被规范化为 access_token 落盘
	raw, err := os.ReadFile(accountPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("invalid stored JSON: %v", err)
	}
	if len(decoded) != 1 || decoded[0]["access_token"] != "alt-token" {
		t.Fatalf("alt key token was not normalized to access_token: %#v", decoded)
	}
}

func TestAccountStoragePreservesJSONCompatibleFields(t *testing.T) {
	root := t.TempDir()
	accountPath := filepath.Join(root, "accounts.json")
	repository := New(accountPath, filepath.Join(root, "auth_keys.json"), filepath.Join(root, "config.json"))

	added, skipped, items, err := repository.AddAccounts(nil, []map[string]any{{
		"access_token":  "token-1",
		"refresh_token": "refresh-secret",
		"email":         "user@example.test",
	}})
	if err != nil || added != 1 || skipped != 0 {
		t.Fatalf("unexpected add result: added=%d skipped=%d err=%v", added, skipped, err)
	}
	if len(items) != 1 || items[0]["access_token"] != "token-1" {
		t.Fatalf("unexpected stored accounts: %#v", items)
	}

	raw, err := os.ReadFile(accountPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("stored account JSON is invalid: %v", err)
	}
	if decoded[0]["refresh_token"] != "refresh-secret" {
		t.Fatalf("account secret was not preserved in storage")
	}
}

func TestValidatorUsesStoredUserKeys(t *testing.T) {
	root := t.TempDir()
	repository := New(filepath.Join(root, "accounts.json"), filepath.Join(root, "auth_keys.json"), filepath.Join(root, "config.json"))
	_, rawKey, err := repository.CreateKey("user", "test", "")
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+rawKey)
	identity, ok := repository.Authenticate(rawKey)
	if !ok || identity.Role != "user" {
		t.Fatalf("stored user key was not accepted")
	}
}

func TestRotateAccountTokensClearsStaleErrorMarkers(t *testing.T) {
	root := t.TempDir()
	repository := New(filepath.Join(root, "accounts.json"), filepath.Join(root, "auth_keys.json"), filepath.Join(root, "config.json"))
	_, _, _, err := repository.AddAccounts(nil, []map[string]any{{
		"access_token":             "old-token",
		"status":                   "异常",
		"status_reason_code":       "auth_invalid",
		"last_refresh_error":       "old refresh failure",
		"last_refresh_error_at":    "2026-08-23T10:00:00Z",
		"last_error_kind":          "auth_invalid",
		"last_error_status":        401,
		"last_error_message":       "expired",
		"last_error_at":            "2026-08-23T10:00:00Z",
		"last_token_refresh_error": "old token failure",
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := repository.RotateAccountTokens(
		"old-token",
		"new-token",
		"new-refresh",
		"",
		map[string]any{"quota": 23, "status": "正常"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated["status"] != "正常" || updated["quota"] != 23 {
		t.Fatalf("remote fields were not persisted: %#v", updated)
	}
	for _, key := range []string{
		"last_refresh_error",
		"last_refresh_error_at",
		"last_error_kind",
		"last_error_status",
		"last_error_message",
		"last_error_at",
		"last_token_refresh_error",
		"last_token_refresh_error_at",
		"status_reason_code",
	} {
		if updated[key] != nil {
			t.Fatalf("stale marker %q was not cleared: %#v", key, updated[key])
		}
	}
}

func TestAccountSnapshotUsesCopyOnWriteAndFlushesRuntimeUpdates(t *testing.T) {
	root := t.TempDir()
	accountPath := filepath.Join(root, "accounts.json")
	repository := New(accountPath, filepath.Join(root, "auth_keys.json"), filepath.Join(root, "config.json"))
	if err := repository.SaveAccounts([]map[string]any{{"access_token": "one", "status": "正常", "enabled": true}}); err != nil {
		t.Fatal(err)
	}

	before, beforeRevision, err := repository.AccountSnapshot()
	if err != nil || len(before) != 1 {
		t.Fatalf("unexpected initial snapshot: %#v %v", before, err)
	}
	if _, err := repository.UpdateAccountRuntime("one", map[string]any{"last_error_status": 502, "last_error_message": "temporary"}); err != nil {
		t.Fatal(err)
	}
	after, afterRevision, err := repository.AccountSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision <= beforeRevision || after[0]["last_error_message"] != "temporary" {
		t.Fatalf("runtime update did not advance snapshot: before=%d after=%d item=%#v", beforeRevision, afterRevision, after[0])
	}
	if before[0]["last_error_message"] != nil {
		t.Fatalf("previous snapshot was mutated in place: %#v", before[0])
	}
	if err := repository.FlushAccounts(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(accountPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted []map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0]["last_error_message"] != "temporary" {
		t.Fatalf("runtime update was not flushed: %#v", persisted)
	}
}
