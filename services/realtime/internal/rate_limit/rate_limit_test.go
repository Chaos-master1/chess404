package rate_limit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newCSRFOkHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestCSRFAllowsExactSelfOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://chess.example/play", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "https://chess.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCSRFRejectsMismatchedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://chess.example/play", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), []string{"https://allowed.example"}, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "CSRF check failed") {
		t.Fatalf("expected CSRF error body, got %s", rr.Body.String())
	}
}

func TestCSRFRejectsOriginWithAttackerSuffix(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://chess.example/play", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "https://chess.example.evil.tld")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), []string{"https://chess.example"}, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for prefix-evasion attempt, got %d", rr.Code)
	}
}

func TestCSRFAllowsViaXForwardedProtoBehindProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://matchmaking-service.railway.internal/api/matchmaking/queues/tickets", nil)
	req.Host = "matchmaking-service.railway.internal"
	req.Header.Set("Origin", "https://web-production-9a697.up.railway.app")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "web-production-9a697.up.railway.app")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 via forwarded headers (plain HTTP = behind proxy), got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCSRFRejectsBadOriginEvenWithForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://internal/api", nil)
	req.Host = "internal"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "public.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), []string{"https://public.example"}, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403: Origin still must be in the allow-list, got %d", rr.Code)
	}
}

func TestCSRFAllowedOriginsListStillTakesPrecedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://alt.example/x", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "https://alt.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), []string{"https://alt.example"}, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from explicit allow-list, got %d", rr.Code)
	}
}

func TestCSRFAllowsPlainGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://chess.example/play", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET, got %d", rr.Code)
	}
}

func TestCSRFRejectsMissingOriginAndReferer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://chess.example/play", nil)
	req.Host = "chess.example"
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when Origin/Referer are absent (CSRF defense), got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCSRFHonorsFirstForwardedProto(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://internal/api", nil)
	req.Host = "internal"
	req.Header.Set("Origin", "https://public.example")
	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "public.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 using first forwarded proto, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCSRFPlainHTTPWithoutForwardedHeadersFallsBackToHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://chess.example/play", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "http://chess.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 when plain HTTP and host matches, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCSRFRejectsPlainHTTPForwardedHeadersButWrongHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://internal/api", nil)
	req.Host = "internal"
	req.Header.Set("Origin", "https://public.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (unlisted origin rejected), got %d", rr.Code)
	}
}

func TestCSRFRejectsUnlistedCrossOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://chess.example/play", nil)
	req.Host = "chess.example"
	req.Header.Set("Origin", "https://web-production-9a697.up.railway.app")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (unlisted cross-origin rejected), got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCSRFFirstForwardedValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://internal/api", nil)
	req.Host = "internal"
	req.Header.Set("Origin", "https://public.example")
	req.Header.Set("X-Forwarded-Proto", " https , http")
	req.Header.Set("X-Forwarded-Host", " public.example , other.example")
	rr := httptest.NewRecorder()
	CSRFMiddleware(newCSRFOkHandler(), nil, "").ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (first forwarded wins), got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareUsesSeparateGlobalAndRouteBuckets(t *testing.T) {
	rl := New()
	defer rl.Close()
	handler := GlobalIPRateLimitMiddleware(rl)(
		MiddlewareWithTrustedBypass(rl, time.Minute, 1)(newCSRFOkHandler()),
	)

	req := httptest.NewRequest(http.MethodGet, "https://chess.example/api/platform/guest-sessions", nil)
	req.RemoteAddr = "203.0.113.20:4123"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected first request to pass separate limiters, got %d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "https://chess.example/api/platform/guest-sessions", nil)
	req.RemoteAddr = "203.0.113.20:4123"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second same-route request to hit route limit, got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareSkipsOptionsAndTrustedServiceRequests(t *testing.T) {
	rl := New()
	defer rl.Close()
	handler := MiddlewareWithTrustedBypass(rl, time.Minute, 1, "internal-token")(newCSRFOkHandler())

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodOptions, "https://chess.example/api/private-matches", nil)
		req.RemoteAddr = "203.0.113.21:4123"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected OPTIONS request %d to bypass rate limit, got %d", i+1, rr.Code)
		}
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "https://chess.example/api/private-matches", nil)
		req.RemoteAddr = "203.0.113.22:4123"
		req.Header.Set("X-Chess404-Service-Token", "internal-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected trusted service request %d to bypass rate limit, got %d", i+1, rr.Code)
		}
	}
}
