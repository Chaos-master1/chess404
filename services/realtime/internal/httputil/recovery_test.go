package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// WithRecovery is the last line of defense against handler panics reaching
// clients. The contract: the panic value must NEVER appear in the response
// (it can carry internal details), the client gets a fixed 500 JSON body, and
// the middleware stays usable for subsequent requests.
func TestWithRecoveryReturnsFixed500WithoutLeakingPanic(t *testing.T) {
	secret := "sentinel-internal-panic-detail"
	handler := WithRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(secret)
	}))
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic must map to 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error":"internal server error"`) {
		t.Fatalf("expected the fixed internal-error JSON body, got %q", body)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("panic value must never leak to the client, got %q", body)
	}
}

func TestWithRecoveryPassthroughOnHappyPath(t *testing.T) {
	handler := WithRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	}))
	req := httptest.NewRequest(http.MethodPost, "/ok", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("non-panicking handler status must pass through, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != `{"created":true}` {
		t.Fatalf("non-panicking handler body must pass through, got %q", got)
	}
}

func TestWithRecoveryStillServesAfterPanic(t *testing.T) {
	var boom bool
	handler := WithRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if boom {
			panic("first request panics")
		}
		w.WriteHeader(http.StatusOK)
	}))

	boom = true
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/x", nil))
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("expected the panicking request to 500, got %d", first.Code)
	}

	boom = false
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/x", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("middleware must keep serving after a recovered panic, got %d", second.Code)
	}
}
