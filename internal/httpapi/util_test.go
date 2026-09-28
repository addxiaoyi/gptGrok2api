package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSafeExportName(t *testing.T) {
	cases := []struct {
		in, fallback, want string
	}{
		{"clean-name_123.pdf", "", "clean-name_123.pdf"},
		{"  spaced name  ", "", "spaced-name"},
		{"", "default.txt", "default.txt"},
		{"a", "b", "a"},
	}
	for _, c := range cases {
		got := safeExportName(c.in, c.fallback)
		if got != c.want {
			t.Fatalf("safeExportName(%q,%q)=%q want %q", c.in, c.fallback, got, c.want)
		}
	}
}

func TestMapValue(t *testing.T) {
	if v := mapValue(map[string]any{"a": 1}); len(v) != 1 {
		t.Fatalf("mapValue should return map")
	}
	if v := mapValue("string"); len(v) != 0 {
		t.Fatalf("mapValue non-map should return empty")
	}
	if v := mapValue(nil); len(v) != 0 {
		t.Fatalf("mapValue nil should return empty")
	}
}

func TestJWTTime(t *testing.T) {
	// unix 0
	if s := jwtTime(float64(0)); s != "" {
		t.Fatalf("jwtTime zero = %q want empty", s)
	}
	// negative
	if s := jwtTime(float64(-1)); s != "" {
		t.Fatalf("jwtTime negative = %q", s)
	}
	// valid epoch
	ts := float64(time.Date(2020,1,2,3,4,5,0,time.UTC).Unix())
	if s := jwtTime(ts); s == "" {
		t.Fatalf("jwtTime valid should not be empty")
	}
}

func TestBackupItem(t *testing.T) {
	// nil info
	item := backupItem("backup.zip", nil)
	if item["key"] != "backup.zip" || item["name"] != "backup.zip" {
		t.Fatalf("backupItem nil info bad")
	}
	// with info
	tmp, err := os.CreateTemp("", "b*.zip")
	if err != nil { t.Fatalf("tmp: %v", err) }
	defer os.Remove(tmp.Name())
	tmp.Close()
	info, _ := os.Stat(tmp.Name())
	item = backupItem("backup.zip", info)
	if item["size"] == nil || item["created_at"] == nil {
		t.Fatalf("backupItem with info missing fields")
	}
	_ = info
}

func TestPublicCPAPool(t *testing.T) {
	p := publicCPAPool(externalCPAPool{ID:"1", Name:"Pool", BaseURL:"https://a.com/"})
	if p["id"] != "1" || p["name"] != "Pool" || p["base_url"] != "https://a.com/" {
		t.Fatalf("publicCPAPool bad: %v", p)
	}
}

func TestPublicSubServer(t *testing.T) {
	s := publicSubServer(externalSubServer{ID:"x", Name:"S", BaseURL:"https://b.com/", APIKey:"k"})
	if s["has_api_key"] != true {
		t.Fatalf("has_api_key false")
	}
}

func TestJobFor(t *testing.T) {
	j := jobFor(5)
	if j["total"] != 5 || j["status"] != "pending" {
		t.Fatalf("jobFor bad %v", j)
	}
}

func TestUpdateJob(t *testing.T) {
	j := map[string]any{"a":1}
	updateJob(j, map[string]any{"b":2})
	if j["b"] != 2 || j["updated_at"] == nil {
		t.Fatalf("updateJob bad")
	}
}

func TestIntValue(t *testing.T) {
	cases := []struct{ in any; want int }{
		{int(5), 5},
		{int64(7), 7},
		{float64(3.9), 3},
		{"42", 42},
		{"bad", 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := intValue(c.in); got != c.want {
			t.Fatalf("intValue(%v)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestMinLen(t *testing.T) {
	if minLen(3,5)!=3 || minLen(5,1)!=1 || minLen(0,0)!=0 {
		t.Fatalf("minLen failed")
	}
}

func TestResourcePath(t *testing.T) {
	prefix := "/api/resources/"
	if a,b := resourcePath("/api/resources/users/123", prefix); a!="users" || b!="123" {
		t.Fatalf("resourcePath got %q %q want users 123", a,b)
	}
	if a,b := resourcePath("/api/resources/status", prefix); a!="status" || b!="" {
		t.Fatalf("resourcePath got %q %q", a,b)
	}
}

func TestUniqueStrings(t *testing.T) {
	in := []string{" a ", "a", "b", " ", "", "b"}
	out := uniqueStrings(in)
	if len(out)!=2 || out[0]!="a" || out[1]!="b" {
		t.Fatalf("uniqueStrings got %v", out)
	}
}

func TestNestedMap(t *testing.T) {
	m := map[string]any{"a": map[string]any{"b":1}}
	if v := nestedMap(m,"a"); len(v)==0 {
		t.Fatalf("nestedMap failed")
	}
	if v := nestedMap(m,"c"); len(v)!=0 {
		t.Fatalf("nestedMap should be empty")
	}
}

func TestCountMonitorStatus(t *testing.T) {
	items := []monitorRecord{{Status:"ok"}, {Status:"err"}, {Status:"ok"}}
	if n := countMonitorStatus(items,"ok"); n!=2 {
		t.Fatalf("countMonitorStatus %d", n)
	}
}

func TestRedactProxyError(t *testing.T) {
	s := redactProxyError("proxy says secret token abc123")
	if len(s)==0 {
		t.Fatalf("redact empty")
	}
	// just ensure it returns non-empty and doesn't panic
	_ = s
}

func TestJWTClaims(t *testing.T) {
	// invalid jwt (no dots)
	if _, err := jwtClaims("invalid"); err == nil {
		t.Fatal("jwtClaims invalid should error")
	}
	// invalid base64
	if _, err := jwtClaims("a.b.c"); err == nil {
		t.Fatal("jwtClaims bad base64 should error")
	}
	// valid jwt
	claims := map[string]any{"sub":"user123","iat":time.Now().Unix()}
	raw, _ := json.Marshal(claims)
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	token := "header." + b64 + ".signature"
	result, err := jwtClaims(token)
	if err != nil || result["sub"] != "user123" {
		t.Fatalf("jwtClaims valid failed: %v %v", result, err)
	}
}

func TestBuildAccountExportItem(t *testing.T) {
	// incomplete account (missing tokens)
	acc := map[string]any{"email":"test@example.com"}
	if _, ok := buildAccountExportItem(acc); ok {
		t.Fatalf("buildAccountExportItem incomplete should fail")
	}
}

func TestSub2APIAccount(t *testing.T) {
	export := map[string]any{
		"access_token": "at123",
		"refresh_token": "rt456",
		"id_token": "id789",
		"email": "test@example.com",
		"account_id": "acct_123",
		"expired": "2025-01-01T00:00:00Z",
		"status": "正常",
	}
	result := sub2APIAccount(export)
	if result["name"] == nil || result["platform"] != "openai" {
		t.Fatalf("sub2APIAccount bad: %v", result)
	}
	creds := result["credentials"].(map[string]any)
	if creds["access_token"] != "at123" {
		t.Fatalf("sub2APIAccount creds bad")
	}
}

func TestAccountExportZip(t *testing.T) {
	items := []map[string]any{
		{"type": "codex", "email": "a@test.com", "account_id": "a1", "access_token": "at", "refresh_token": "rt", "id_token": "id", "status": "正常"},
		{"type": "codex", "email": "b@test.com", "account_id": "b2", "access_token": "at2", "refresh_token": "rt2", "id_token": "id2", "status": "正常"},
	}
	data, err := accountExportZip(items)
	if err != nil {
		t.Fatalf("accountExportZip failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("accountExportZip empty")
	}
}

func TestSub2APIProxiesAndPlanType(t *testing.T) {
	accounts := []map[string]any{
		{"access_token": "at", "email": "a@test.com", "proxy": "http://proxy1:8080", "plan_type": "chatgpt_plus"},
		{"access_token": "bt", "email": "b@test.com", "proxy": "http://proxy1:8080", "plan_type": "free"},
		{"access_token": "ct", "email": "c@test.com"},
	}
	proxies := collectSub2APIProxies(accounts)
	if len(proxies) != 1 {
		t.Fatalf("expected 1 unique proxy, got %d: %v", len(proxies), proxies)
	}
	if proxies[0] != "http://proxy1:8080" {
		t.Fatalf("unexpected proxy: %v", proxies[0])
	}
	acc := sub2APIAccount(accounts[0])
	extra := acc["extra"].(map[string]any)
	if extra["proxy"] != "http://proxy1:8080" {
		t.Fatalf("expected proxy in extra, got %v", extra["proxy"])
	}
	if extra["plan_type"] != "chatgpt_plus" {
		t.Fatalf("expected plan_type in extra, got %v", extra["plan_type"])
	}
}

func TestNestedStringValue(t *testing.T) {
	item := map[string]any{"fields": map[string]any{"proxy": "http://nested:8080"}}
	if v := nestedStringValue(item, "fields", "proxy"); v != "http://nested:8080" {
		t.Fatalf("nestedStringValue got %q", v)
	}
	if v := nestedStringValue(item, "missing", "key"); v != "" {
		t.Fatalf("nestedStringValue should return empty for missing key, got %q", v)
	}
}

func TestWriteDownloadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeDownloadJSON(rec, "test.json", map[string]any{"key":"value"})
	if rec.Code != 200 {
		t.Fatalf("writeDownloadJSON status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "value") {
		t.Fatalf("writeDownloadJSON body bad: %s", rec.Body.String())
	}
}

func TestAnyList(t *testing.T) {
	if v := anyList([]any{1,2}); len(v)!=2 { t.Fatalf("anyList bad") }
	if v := anyList("x"); len(v)!=0 { t.Fatalf("anyList non-list should be empty") }
}

func TestMonitorNumber(t *testing.T) {
	cases := []struct{in any; want int64}{
		{int(5),5},{int32(7),7},{int64(9),9},{float64(3.9),3},{float32(2.1),2},{"x",0},
	}
	for _, c := range cases {
		if got := monitorNumber(c.in); got != c.want {
			t.Fatalf("monitorNumber(%v)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestMonitorPercentile(t *testing.T) {
	values := []int64{1,2,3,4,5}
	if p := monitorPercentile(values, 0.5); p != 3 { t.Fatalf("p50 %d", p) }
	if p := monitorPercentile(values, 1.0); p != 5 { t.Fatalf("p100 %d", p) }
	if p := monitorPercentile([]int64{}, 0.5); p != 0 { t.Fatalf("empty percentile") }
}

func TestMonitorMetricValue(t *testing.T) {
	item := map[string]any{"metrics": map[string]any{"lat":120}}
	if v := monitorMetricValue(item, "lat"); v != 120 {
		t.Fatalf("metric value %d", v)
	}
	if v := monitorMetricValue(map[string]any{}, "x"); v != 0 {
		t.Fatalf("metric missing")
	}
}
