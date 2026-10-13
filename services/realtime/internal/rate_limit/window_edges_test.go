package rate_limit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestLimiter returns an InMemoryRateLimiter with a controllable clock so
// window-boundary behavior is deterministic (no sleeps).
func newTestLimiter() (*InMemoryRateLimiter, *time.Time) {
	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := base
	l := &InMemoryRateLimiter{
		now:     func() time.Time { return now },
		buckets: make(map[string]bucket),
		stopCh:  make(chan struct{}),
	}
	return l, &now
}

func advance(nowp *time.Time, d time.Duration) { *nowp = nowp.Add(d) }

func TestInMemoryAllowExactWindowBoundaryResets(t *testing.T) {
	l, nowp := newTestLimiter()
	const window = time.Minute
	const limit = 3

	for i := 0; i < limit; i++ {
		if allowed, _ := l.Allow("k", window, limit); !allowed {
			t.Fatalf("request %d of %d should be allowed", i+1, limit)
		}
	}
	// The very last instant of the window: still exhausted.
	advance(nowp, window-time.Nanosecond)
	if allowed, retry := l.Allow("k", window, limit); allowed {
		t.Fatal("request at window-end should be denied")
	} else if retry <= 0 {
		t.Fatalf("retryAfter should be positive, got %v", retry)
	}
	// Exactly one full window after the bucket opened: fresh bucket.
	advance(nowp, time.Nanosecond)
	if allowed, retry := l.Allow("k", window, limit); !allowed {
		t.Fatalf("request at exact window rollover should be allowed (retryAfter=%v)", retry)
	}
}

func TestInMemoryRetryAfterIsAtLeastOneSecond(t *testing.T) {
	l, nowp := newTestLimiter()
	// Burn the budget, then jump almost to the window end so the raw
	// remaining time is <1s; the limiter must clamp to 1s.
	const window = time.Minute
	_, _ = l.Allow("k", window, 1)
	advance(nowp, window-500*time.Millisecond)
	allowed, retry := l.Allow("k", window, 1)
	if allowed {
		t.Fatal("request should be denied")
	}
	if retry < time.Second {
		t.Fatalf("retryAfter must be clamped to >=1s, got %v", retry)
	}
}

func TestInMemoryEmptyKeyOrNonPositiveLimitAlwaysAllows(t *testing.T) {
	l, _ := newTestLimiter()
	if allowed, _ := l.Allow("", time.Minute, 10); !allowed {
		t.Fatal("empty key must bypass the limiter")
	}
	if allowed, _ := l.Allow("k", time.Minute, 0); !allowed {
		t.Fatal("limit<=0 must bypass the limiter")
	}
	if allowed, _ := l.Allow("k", time.Minute, -5); !allowed {
		t.Fatal("negative limit must bypass the limiter")
	}
}

func TestInMemoryIndependentKeys(t *testing.T) {
	l, _ := newTestLimiter()
	const window = time.Minute
	// Exhaust key A; key B must be untouched. Bucket identity is the key
	// string alone (the window only controls expiry), so this also pins
	// that exhausting A does not leak into differently-named keys.
	for i := 0; i < 5; i++ {
		_, _ = l.Allow("A", window, 5)
	}
	if allowed, _ := l.Allow("B", window, 5); !allowed {
		t.Fatal("exhausting key A must not affect key B")
	}
	if allowed, _ := l.Allow("A", window, 5); allowed {
		t.Fatal("key A must still be exhausted")
	}
}

func TestContentTypeMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := ContentTypeMiddleware(next)

	cases := []struct {
		name   string
		method string
		ct     string
		want   int
	}{
		{"POST missing content-type", http.MethodPost, "", http.StatusUnsupportedMediaType},
		{"POST wrong content-type", http.MethodPost, "text/plain", http.StatusUnsupportedMediaType},
		{"POST json with charset", http.MethodPost, "application/json; charset=utf-8", http.StatusOK},
		{"GET needs no content-type", http.MethodGet, "", http.StatusOK},
		{"PUT json", http.MethodPut, "application/json", http.StatusOK},
		{"PATCH wrong content-type", http.MethodPatch, "application/xml", http.StatusUnsupportedMediaType},
		{"DELETE needs no content-type", http.MethodDelete, "", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, "http://svc/api/x", nil)
		if tc.ct != "" {
			req.Header.Set("Content-Type", tc.ct)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, rr.Code, tc.want)
		}
	}
}

func TestSecurityHeadersMiddlewareSetsHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "http://svc/", nil)
	rr := httptest.NewRecorder()
	SecurityHeadersMiddleware(next).ServeHTTP(rr, req)

	for _, h := range []string{"Content-Security-Policy", "Strict-Transport-Security", "X-Content-Type-Options"} {
		if rr.Header().Get(h) == "" {
			t.Fatalf("security header %s missing", h)
		}
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestWriteRateLimitExceededSetsRetryAfter(t *testing.T) {
	rr := httptest.NewRecorder()
	writeRateLimitExceeded(rr, 2500*time.Millisecond)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d want 429", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want 3 (ceil of 2.5s)", got)
	}
	// Sub-second remaining time clamps to 1.
	rr2 := httptest.NewRecorder()
	writeRateLimitExceeded(rr2, 10*time.Millisecond)
	if got := rr2.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

func TestGlobalIPRateLimitKeyIgnoresPathAndMethod(t *testing.T) {
	req1 := httptest.NewRequest(http.MethodGet, "http://svc/a", nil)
	req2 := httptest.NewRequest(http.MethodPost, "http://svc/b", nil)
	k1 := rateLimitKey("1.2.3.4", req1, true)
	k2 := rateLimitKey("1.2.3.4", req2, true)
	if k1 != k2 {
		t.Fatalf("global keys must be identical across paths/methods: %q vs %q", k1, k2)
	}
	if k3 := rateLimitKey("5.6.7.8", req1, true); k3 == k1 {
		t.Fatal("different IPs must map to different global keys")
	}
}

func TestRouteRateLimitKeySeparatesMethodAndPath(t *testing.T) {
	req1 := httptest.NewRequest(http.MethodGet, "http://svc/a", nil)
	req2 := httptest.NewRequest(http.MethodPost, "http://svc/a", nil)
	req3 := httptest.NewRequest(http.MethodGet, "http://svc/b", nil)
	keys := map[string]bool{}
	for _, req := range []*http.Request{req1, req2, req3} {
		k := rateLimitKey("1.2.3.4", req, false)
		if keys[k] {
			t.Fatalf("route key %q repeated across differing requests", k)
		}
		keys[k] = true
	}
}

func TestClientIPDirectRemoteAddrWithoutTrustedHeaders(t *testing.T) {
	t.Setenv("TRUST_FORWARDED_HEADERS", "")
	t.Setenv("RAILWAY_ENVIRONMENT", "")
	req := httptest.NewRequest(http.MethodGet, "http://svc/", nil)
	req.RemoteAddr = "9.9.9.9:55555"
	req.Header.Set("X-Forwarded-For", "1.1.1.1") // must be ignored when not trusted
	if got := ClientIP(req); got != "9.9.9.9" {
		t.Fatalf("ClientIP = %q, want 9.9.9.9 (spoofed header ignored)", got)
	}
}

func TestClientIPTrustsForwardedHeadersWhenEnabled(t *testing.T) {
	t.Setenv("TRUST_FORWARDED_HEADERS", "1")
	req := httptest.NewRequest(http.MethodGet, "http://svc/", nil)
	req.RemoteAddr = "9.9.9.9:55555"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := ClientIP(req); got != "1.1.1.1" {
		t.Fatalf("ClientIP = %q, want 1.1.1.1 (first hop of trusted XFF)", got)
	}
	req.Header.Set("X-Railway-Client-Ip", "3.3.3.3")
	if got := ClientIP(req); got != "3.3.3.3" {
		t.Fatalf("ClientIP = %q, want 3.3.3.3 (Railway header precedence)", got)
	}
}

func TestClientIPMalformedRemoteAddrReturnsRawValue(t *testing.T) {
	t.Setenv("TRUST_FORWARDED_HEADERS", "")
	t.Setenv("RAILWAY_ENVIRONMENT", "")
	req := httptest.NewRequest(http.MethodGet, "http://svc/", nil)
	req.RemoteAddr = "not-an-addr"
	if got := ClientIP(req); got != "not-an-addr" {
		t.Fatalf("ClientIP = %q, want raw fallback", got)
	}
	if got := ClientIP(nil); got != "" {
		t.Fatalf("ClientIP(nil) = %q, want empty", got)
	}
}

func TestInMemoryBurstAcrossBoundaryThenDeny(t *testing.T) {
	l, nowp := newTestLimiter()
	const window = time.Second
	const limit = 2

	_, _ = l.Allow("k", window, limit)
	advance(nowp, window) // rollover, fresh bucket
	if !allowN(l, "k", window, limit, limit) {
		t.Fatalf("fresh window must admit %d requests", limit)
	}
	if allowed, _ := l.Allow("k", window, limit); allowed {
		t.Fatal("burst beyond limit in fresh window must be denied")
	}
}

func allowN(l *InMemoryRateLimiter, key string, window time.Duration, limit, n int) bool {
	for i := 0; i < n; i++ {
		if allowed, _ := l.Allow(key, window, limit); !allowed {
			return false
		}
	}
	return true
}
