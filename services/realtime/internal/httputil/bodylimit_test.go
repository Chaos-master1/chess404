package httputil

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// LimitBody is the request-size DoS guard shared by every JSON route: it caps
// request bodies at 1 MiB via http.MaxBytesReader. Both sides of the boundary
// are security-relevant -- a body exactly at the cap must be fully readable,
// and one byte over must fail reads (the handler's signal to answer 413)
// rather than silently truncating into a parseable half-request.
func TestLimitBodyAllowsRequestAtCap(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 1<<20) // exactly the 1 MiB cap
	var got int
	var readErr error
	handler := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		got = int(n)
		readErr = err
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if readErr != nil {
		t.Fatalf("body exactly at the cap must read cleanly, got error: %v", readErr)
	}
	if got != len(body) {
		t.Fatalf("expected to read %d bytes at the cap, got %d", len(body), got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 at the cap, got %d", rec.Code)
	}
}

func TestLimitBodyRejectsOversizedRequest(t *testing.T) {
	body := bytes.Repeat([]byte("a"), (1<<20)+1) // one byte over the cap
	var got int
	var readErr error
	handler := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		got = int(n)
		readErr = err
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatalf("a body over the cap must fail reads, got a clean read of %d bytes", got)
	}
	if got >= len(body) {
		t.Fatalf("an oversized body must not be fully readable, got %d/%d bytes", got, len(body))
	}
}

func TestLimitBodyAllowsSmallRequest(t *testing.T) {
	payload := `{"hello":"world"}`
	var got string
	handler := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("small body must read cleanly, got %v", err)
		}
		got = string(b)
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(payload))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if got != payload {
		t.Fatalf("small body must pass through unchanged, got %q", got)
	}
}
