package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRedactURLCredentialsEmpty(t *testing.T) {
	if got := RedactURLCredentials(""); got != "" {
		t.Fatalf("empty input should return empty, got %q", got)
	}
}

func TestRedactURLCredentialsNoUserInfo(t *testing.T) {
	in := "https://api.example.com/x"
	if got := RedactURLCredentials(in); got != in {
		t.Fatalf("URL without user-info should pass through, got %q", got)
	}
}

func TestRedactURLCredentialsWithPassword(t *testing.T) {
	in := "redis://default:sRHGauPpO0EVyr1CsBPHkOfOBFlnSZnCT@redis.railway.internal:6379"
	want := "redis://default:REDACTED@redis.railway.internal:6379"
	if got := RedactURLCredentials(in); got != want {
		t.Fatalf("password should be redacted, got %q want %q", got, want)
	}
}

func TestRedactURLCredentialsUsernameOnly(t *testing.T) {
	in := "redis://default@redis.railway.internal:6379"
	if got := RedactURLCredentials(in); got != in {
		t.Fatalf("URL with username only (no password) should pass through, got %q", got)
	}
}

func TestRedactURLCredentialsMalformed(t *testing.T) {
	in := "://not a url"
	if got := RedactURLCredentials(in); got != "<unparseable-url>" {
		t.Fatalf("malformed URL should return placeholder, got %q", got)
	}
}

// EnvOrDefault backs every service's configuration; a whitespace-only value
// must behave like an unset one (a stray " " in an env var must not win over
// the fallback).
func TestEnvOrDefault(t *testing.T) {
	t.Setenv("TEST_HTTPUTIL_ENV", "value")
	if got := EnvOrDefault("TEST_HTTPUTIL_ENV", "fallback"); got != "value" {
		t.Fatalf("set env must win, got %q", got)
	}

	t.Setenv("TEST_HTTPUTIL_ENV", "   ")
	if got := EnvOrDefault("TEST_HTTPUTIL_ENV", "fallback"); got != "fallback" {
		t.Fatalf("whitespace-only env must fall back, got %q", got)
	}

	t.Setenv("TEST_HTTPUTIL_ENV", "")
	if got := EnvOrDefault("TEST_HTTPUTIL_ENV", "fallback"); got != "fallback" {
		t.Fatalf("empty env must fall back, got %q", got)
	}
}

func TestListenAddrPrecedence(t *testing.T) {
	t.Setenv("TEST_HTTPUTIL_ADDR", "127.0.0.1:7001")
	t.Setenv("PORT", "7002")
	if got := ListenAddr("TEST_HTTPUTIL_ADDR", 7003); got != "127.0.0.1:7001" {
		t.Fatalf("explicit addr env must take precedence, got %q", got)
	}

	t.Setenv("TEST_HTTPUTIL_ADDR", "   ")
	if got := ListenAddr("TEST_HTTPUTIL_ADDR", 7003); got != ":7002" {
		t.Fatalf("PORT must be used when the addr env is blank, got %q", got)
	}

	t.Setenv("PORT", "")
	if got := ListenAddr("TEST_HTTPUTIL_ADDR", 7003); got != ":7003" {
		t.Fatalf("default port must be used last, got %q", got)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{
		0:    "0",
		7:    "7",
		8080: "8080",
		-1:   "-1",
		-909: "-909",
	}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

// ParseAllowedOrigins + IsOriginAllowed implement the CORS allow-list. The
// security contract: an empty allow-list admits nothing, matching is
// case-insensitive exact-match only (no prefix/suffix/scheme-relaxed matches),
// and blank list entries are dropped rather than becoming a match-all "".
func TestParseAllowedOrigins(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", " https://a.example , https://b.example ,,  ")
	got := ParseAllowedOrigins()
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("expected trimmed non-empty origins only, got %#v", got)
	}

	t.Setenv("ALLOWED_ORIGINS", "")
	if got := ParseAllowedOrigins(); len(got) != 0 {
		t.Fatalf("unset allow-list must parse to no origins, got %#v", got)
	}
}

func TestIsOriginAllowed(t *testing.T) {
	if IsOriginAllowed("https://evil.example", nil) {
		t.Fatalf("empty allow-list must admit nothing")
	}
	allowed := []string{"https://Play.example", "https://b.example"}
	if !IsOriginAllowed("https://play.example", allowed) {
		t.Fatalf("origin comparison must be case-insensitive")
	}
	if !IsOriginAllowed("https://PLAY.EXAMPLE", allowed) {
		t.Fatalf("origin comparison must be case-insensitive (receiver side)")
	}
	if IsOriginAllowed("https://b.example.evil.com", allowed) {
		t.Fatalf("suffix-padded origins must not match")
	}
	if IsOriginAllowed("https://evil.com/?x=https://b.example", allowed) {
		t.Fatalf("origin strings are compared literally, never parsed permissively")
	}
	if IsOriginAllowed("http://b.example", allowed) {
		t.Fatalf("scheme changes must not match")
	}
}

func TestWriteJSONShapeAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusTeapot, map[string]any{"ok": true})

	if rec.Code != http.StatusTeapot {
		t.Fatalf("WriteJSON must honor the status, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("WriteJSON must set application/json, got %q", ct)
	}
	var parsed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("WriteJSON body must be valid JSON, got %q: %v", rec.Body.String(), err)
	}
	if parsed["ok"] != true {
		t.Fatalf("unexpected parsed body: %#v", parsed)
	}
}

func TestWriteErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "nope")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("WriteError must honor the status, got %d", rec.Code)
	}
	if got, want := rec.Body.String(), "{\"error\":\"nope\"}\n"; got != want {
		t.Fatalf("WriteError body = %q, want %q", got, want)
	}
}

func TestNowUTC(t *testing.T) {
	if _, offset := NowUTC().Zone(); offset != 0 {
		t.Fatalf("NowUTC must be UTC, got offset %d", offset)
	}
	if NowUTC().Location() != time.UTC {
		t.Fatalf("NowUTC location must be time.UTC")
	}
}
