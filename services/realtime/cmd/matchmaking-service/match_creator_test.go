package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chess404/realtime/internal/matchmaking"
	"github.com/chess404/realtime/internal/rate_limit"
)

// TestHTTPMatchCreatorSendsServiceToken pins the header wiring: room
// creations must present the destination's service token. A creator built
// without a token (the pre-fix state) sends no header and gets throttled by
// match-service's global per-IP limiter under pairing bursts.
func TestHTTPMatchCreatorSendsServiceToken(t *testing.T) {
	var gotToken atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/api/matches", func(w http.ResponseWriter, r *http.Request) {
		gotToken.Store(r.Header.Get("X-Chess404-Service-Token"))
		w.WriteHeader(http.StatusCreated)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	assignment := matchmaking.MatchAssignment{RoomID: "room_token_check"}

	withToken := &httpMatchCreator{baseURL: server.URL, serviceToken: "match-specific"}
	if err := withToken.CreateMatch(assignment); err != nil {
		t.Fatalf("expected CreateMatch with service token to succeed, got %v", err)
	}
	if got, _ := gotToken.Load().(string); got != "match-specific" {
		t.Fatalf("expected X-Chess404-Service-Token %q to be sent, got %q", "match-specific", got)
	}

	withoutToken := &httpMatchCreator{baseURL: server.URL}
	if err := withoutToken.CreateMatch(assignment); err != nil {
		t.Fatalf("expected CreateMatch without configured token to still succeed (header simply omitted), got %v", err)
	}
	if got, _ := gotToken.Load().(string); got != "" {
		t.Fatalf("expected no X-Chess404-Service-Token when none is configured, got %q", got)
	}
}

// TestHTTPMatchCreatorBurstSurvivesGlobalIPRateLimit reproduces the
// production failure behind the 20/100-pair soak regressions: a burst of
// room creations from one IP against match-service's real global per-IP
// limiter (60/min). With the service token the burst sails through; without
// it the limiter 429s and every rejection rolls the pair back to queued.
func TestHTTPMatchCreatorBurstSurvivesGlobalIPRateLimit(t *testing.T) {
	newThrottledServer := func(rl rate_limit.RateLimiter) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/matches", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
		return httptest.NewServer(rate_limit.GlobalIPRateLimitMiddleware(rl, "expected-token")(mux))
	}
	assignment := matchmaking.MatchAssignment{RoomID: "room_burst_check"}

	t.Run("with service token bypasses global per-IP limit", func(t *testing.T) {
		rl, err := rate_limit.NewRateLimiter()
		if err != nil {
			t.Fatalf("expected rate limiter to initialize, got %v", err)
		}
		defer rl.Close()
		server := newThrottledServer(rl)
		defer server.Close()

		creator := &httpMatchCreator{baseURL: server.URL, serviceToken: "expected-token"}
		for i := 0; i < 100; i++ {
			if err := creator.CreateMatch(assignment); err != nil {
				t.Fatalf("expected trusted creation %d to bypass the global per-IP limit, got %v", i+1, err)
			}
		}
	})

	t.Run("without service token the global per-IP limit throttles the burst", func(t *testing.T) {
		rl, err := rate_limit.NewRateLimiter()
		if err != nil {
			t.Fatalf("expected rate limiter to initialize, got %v", err)
		}
		defer rl.Close()
		server := newThrottledServer(rl)
		defer server.Close()

		creator := &httpMatchCreator{baseURL: server.URL}
		throttled := 0
		for i := 0; i < 100; i++ {
			if err := creator.CreateMatch(assignment); err != nil {
				if !strings.Contains(err.Error(), "status 429") {
					t.Fatalf("expected 429 rate-limit error, got %v", err)
				}
				throttled++
			}
		}
		if throttled < 30 {
			t.Fatalf("expected the un-tokened burst to hit the 60/min global limit, got only %d throttled creations", throttled)
		}
	})
}

// TestMatchServiceCallerTokenPrecedence mirrors platformServiceCallerToken's
// contract: the destination's accept list, in precedence order, independent
// of this service's inbound chain.
func TestMatchServiceCallerTokenPrecedence(t *testing.T) {
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "match-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "")
	t.Setenv("CHESS404_INTERNAL_SERVICE_TOKEN", "shared")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "")
	if got := matchServiceCallerToken(); got != "match-specific" {
		t.Fatalf("expected service-specific token to win, got %q", got)
	}

	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "")
	if got := matchServiceCallerToken(); got != "shared" {
		t.Fatalf("expected shared token fallback, got %q", got)
	}

	t.Setenv("CHESS404_INTERNAL_SERVICE_TOKEN", "")
	if got := matchServiceCallerToken(); got != "" {
		t.Fatalf("expected empty token when no env is set, got %q", got)
	}
}
