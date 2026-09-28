package provider

import (
	"testing"
)

func TestExtensionForMIME(t *testing.T) {
	cases := []struct{ in, want string }{
		{"image/png", ".png"},
		{"image/webp", ".webp"},
		{"image/jpg", ".jpg"},
		{"image/jpg; charset=utf-8", ".jpg"},
		{"", ".jpg"},
	}
	for _, c := range cases {
		if got := extensionForMIME(c.in); got != c.want {
			t.Fatalf("extensionForMIME(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestAbsoluteAssetURL(t *testing.T) {
	if got := absoluteAssetURL("https://example.com/a"); got != "https://example.com/a" {
		t.Fatalf("absoluteAssetURL https failed")
	}
	if got := absoluteAssetURL("/b/c"); got != "https://assets.grok.com/b/c" {
		t.Fatalf("absoluteAssetURL rel failed, got %q", got)
	}
	if got := absoluteAssetURL("d/e"); got != "https://assets.grok.com/d/e" {
		t.Fatalf("absoluteAssetURL plain failed")
	}
}

func TestQuotaNumber(t *testing.T) {
	cases := []struct{ in any; want int; ok bool }{
		{float64(10),10,true},
		{int(5),5,true},
		{"42",42,true},
		{"bad",0,false},
		{nil,0,false},
	}
	for _, c := range cases {
		got, ok := quotaNumber(c.in)
		if ok != c.ok || got != c.want {
			t.Fatalf("quotaNumber(%v)=%d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestIsGrokInvalidBody(t *testing.T) {
	markers := []string{"invalid-credentials", "bad-credentials", "session not found", "token revoked", "token expired", "account suspended", "blocked-user"}
	for _, m := range markers {
		if !isGrokInvalidBody("error: " + m) {
			t.Fatalf("marker %q should be detected", m)
		}
	}
	if isGrokInvalidBody("some other error") {
		t.Fatal("non-marker should not match")
	}
}
