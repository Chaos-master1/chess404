package httputil

import (
	"bufio"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// WithLogging fronts every service route. The request-id contract is used by
// log correlation and by downstream handlers (RequestIDFromContext), and the
// recorder must transparently support Write-without-WriteHeader, Flush
// (SSE/streaming snapshots) and Hijack (WebSocket upgrades) or those paths
// would break the moment logging middleware is added.
func TestWithLoggingGeneratesRequestIDWhenAbsent(t *testing.T) {
	var seenViaContext string
	handler := WithLogging("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenViaContext = RequestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-Id")
	if id == "" {
		t.Fatalf("a request without X-Request-Id must get a generated one")
	}
	if len(id) != 32 {
		t.Fatalf("generated request id should be 16 random bytes hex-encoded (32 chars), got %q", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("generated request id must be hex, got %q: %v", id, err)
	}
	if seenViaContext != id {
		t.Fatalf("handler must see the generated id via context, got %q want %q", seenViaContext, id)
	}
}

func TestWithLoggingPreservesClientRequestID(t *testing.T) {
	var seenViaContext string
	handler := WithLogging("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenViaContext = RequestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "client-provided-id-42")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "client-provided-id-42" {
		t.Fatalf("client-provided X-Request-Id must be echoed, got %q", got)
	}
	if seenViaContext != "client-provided-id-42" {
		t.Fatalf("handler must see the client id via context, got %q", seenViaContext)
	}
}

func TestWithLoggingRecordsExplicitStatus(t *testing.T) {
	handler := WithLogging("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("later"))
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("explicit status must pass through, got %d", rec.Code)
	}
}

func TestWithLoggingDefaultsStatusTo200OnBareWrite(t *testing.T) {
	// Exercises responseRecorder.Write's status defaulting: a handler that
	// writes a body without calling WriteHeader must count as 200, not 0.
	handler := WithLogging("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("bare"))
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("bare Write must be recorded as 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "bare" {
		t.Fatalf("body must pass through the recorder, got %q", got)
	}
}

func TestWithLoggingFlushPassesThrough(t *testing.T) {
	handler := WithLogging("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	underlying := httptest.NewRecorder()
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/x", nil))
	if !underlying.Flushed {
		t.Fatalf("Flush through the recorder must reach the underlying Flusher")
	}
}

// hijackRecorder implements http.Hijacker over an httptest.ResponseRecorder so
// the positive Hijack delegation path is testable without a real TCP socket.
type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestResponseRecorderHijackDelegatesWhenSupported(t *testing.T) {
	underlying := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := &responseRecorder{ResponseWriter: underlying, status: http.StatusOK}

	conn, rw, err := rec.Hijack()
	if err != nil {
		t.Fatalf("Hijack must delegate when the writer supports it, got error: %v", err)
	}
	if conn != nil || rw != nil {
		t.Fatalf("expected the marker implementation's nils, got conn=%v rw=%v", conn, rw)
	}
	if !underlying.hijacked {
		t.Fatalf("Hijack must reach the underlying hijacker")
	}
}

func TestResponseRecorderHijackErrorsWhenUnsupported(t *testing.T) {
	// httptest.ResponseRecorder does not implement http.Hijacker. The recorder
	// must surface an error instead of panicking -- WS upgrade handlers rely
	// on checking this error.
	rec := &responseRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	conn, rw, err := rec.Hijack()
	if err == nil {
		t.Fatalf("Hijack on a non-hijackable writer must return an error")
	}
	if conn != nil || rw != nil {
		t.Fatalf("failed Hijack must return nils, got conn=%v rw=%v", conn, rw)
	}
}

func TestRequestIDContextRoundTrip(t *testing.T) {
	if got := RequestIDFromContext(t.Context()); got != "" {
		t.Fatalf("empty context must yield empty request id, got %q", got)
	}
	if got := RequestIDFromContext(WithRequestID(t.Context(), "abc")); got != "abc" {
		t.Fatalf("WithRequestID/RequestIDFromContext round trip failed, got %q", got)
	}
}
