package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/match"
	"github.com/chess404/realtime/internal/platform"
	"github.com/gorilla/websocket"
)

func TestFinalizingArchiveStoreCallsPlatformForFinishedRatedMatch(t *testing.T) {
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = archive.Close() }()

	called := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/internal/finalize-rated-match" {
			t.Errorf("unexpected finalizer path %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Chess404-Service-Token"); got != "service-secret" {
			t.Errorf("expected service token header, got %q", got)
		}
		var payload struct {
			MatchID string `json:"matchId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("expected finalizer payload to decode, got %v", err)
		}
		called <- payload.MatchID
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"changed":true}`))
	}))
	defer server.Close()

	store := &finalizingArchiveStore{
		archive:      archive,
		platformURL:  server.URL,
		serviceToken: "service-secret",
		client:       server.Client(),
		inFlight:     make(map[string]struct{}),
		done:         make(map[string]struct{}),
	}
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	if err := store.Upsert(contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      "rated_finish",
			RulesVersion: "v1-alpha-foundation",
			Queue:        "rated",
			Status:       "finished",
			Winner:       "white",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}); err != nil {
		t.Fatalf("expected archive upsert to succeed, got %v", err)
	}

	select {
	case matchID := <-called:
		if matchID != "rated_finish" {
			t.Fatalf("expected finalizer to receive match id, got %q", matchID)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected platform finalizer to be called")
	}
}

// withCORSPreflightHeaders is the set of headers the browser sends in an Access-Control-Request-Headers
// preflight when calling a match endpoint with identity credentials.
var withCORSPreflightHeaders = []string{
	"X-Chess404-White-Guest-Id",
	"X-Chess404-White-Session-Token",
	"X-Chess404-Black-Guest-Id",
	"X-Chess404-Black-Session-Token",
}

func TestWithCORSPreflightAllowsChess404Headers(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://web-production-9a697.up.railway.app")

	called := false
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, hdr := range withCORSPreflightHeaders {
		req := httptest.NewRequest(http.MethodOptions, "/api/matches/room_test", nil)
		req.Header.Set("Origin", "https://web-production-9a697.up.railway.app")
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", hdr)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("header %s: expected 204, got %d", hdr, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://web-production-9a697.up.railway.app" {
			t.Fatalf("header %s: expected allowed origin to echo, got %q", hdr, got)
		}
		allow := rec.Header().Get("Access-Control-Allow-Headers")
		if !strings.Contains(strings.ToLower(allow), strings.ToLower(hdr)) {
			t.Fatalf("header %s: expected Allow-Headers to include %s, got %q", hdr, strings.ToLower(hdr), allow)
		}
	}
	if called {
		t.Fatalf("OPTIONS request must not invoke next handler")
	}
}

func TestWithCORSRejectsUnknownOriginWithoutAllowOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://web-production-9a697.up.railway.app")

	called := false
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/matches/room_test", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatalf("CORS middleware does not block server-side; the browser is what enforces it. The next handler should still run.")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (passthrough), got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Allow-Origin for disallowed origin, got %q", got)
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("expected Vary: Origin to be set")
	}
}

// TestJoinMatchSeatHTTPResponseRedactsSeatSecrets is a regression test for a
// live production bug: the POST .../join HTTP handler wrote JoinMatchSeat's
// response straight to JSON, unlike the sibling POST /api/matches (create)
// and POST .../intents handlers, which both call match.RedactSnapshotSecrets
// first. JoinMatchSeat's raw response necessarily carries both seats' secrets
// (needed internally to persist/broadcast correctly), so this is exactly the
// case where a copy-pasted handler missing one line quietly hands the second
// player the FIRST player's seat secret -- full control of their opponent's
// seat (resign, move, anything) -- on their very first request. Goes through
// the real mux via buildMatchServiceMux, not service.JoinMatchSeat directly,
// because the bug was in the HTTP handler's wiring, not in match.Service.
func TestJoinMatchSeatHTTPResponseRedactsSeatSecrets(t *testing.T) {
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = archive.Close() }()

	service := match.NewService()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "join_secret_leak_test",
		Queue:             "direct",
		WhiteGuestID:      "guest_white_first",
		WhitePlayerSecret: "white-first-players-real-secret",
	}, now)

	mux := buildMatchServiceMux(service, archive, websocket.Upgrader{}, 64*1024)

	body := `{"guestId":"guest_black_second","playerSecret":"black-second-players-secret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/matches/join_secret_leak_test/join", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected join to succeed, got status %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "white-first-players-real-secret") {
		t.Fatalf("join response leaked the first player's seat secret to the second player joining: %s", rec.Body.String())
	}

	var resp contracts.JoinMatchSeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected join response to decode, got %v", err)
	}
	if resp.Match.Match.WhitePlayerSecret != "" || resp.Match.Match.BlackPlayerSecret != "" {
		t.Fatalf("expected both seat secrets to be redacted from the join HTTP response, got white=%q black=%q",
			resp.Match.Match.WhitePlayerSecret, resp.Match.Match.BlackPlayerSecret)
	}
}

// TestSeatSecretEndpointDeliversCredentialOnlyToTrustedCaller covers the
// internal seat-secret endpoint used by the platform claim pipeline. Queue-
// matched rooms are created with server-generated seat secrets that no client
// ever receives; this endpoint is the one trusted path that hands the real
// credential back (service-token gated, computer-match seats rejected).
func TestSeatSecretEndpointDeliversCredentialOnlyToTrustedCaller(t *testing.T) {
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = archive.Close() }()

	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")

	service := match.NewService()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "seat_secret_delivery_test",
		Queue:             "casual",
		WhiteGuestID:      "guest_white_queued",
		WhitePlayerSecret: "server-generated-white-secret",
		BlackGuestID:      "guest_black_queued",
		BlackPlayerSecret: "server-generated-black-secret",
	}, now)

	mux := buildMatchServiceMux(service, archive, websocket.Upgrader{}, 64*1024)

	postSeatSecret := func(guestID string, token string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"guestId":%q}`, guestID)
		req := httptest.NewRequest(http.MethodPost, "/api/matches/seat_secret_delivery_test/seat-secret", strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Chess404-Service-Token", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Without the service token the endpoint must refuse (401), even for a
	// legitimately seated guest -- this is the client-facing attack surface.
	if rec := postSeatSecret("guest_white_queued", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without service token, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postSeatSecret("guest_white_queued", "wrong-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a wrong service token, got %d body=%s", rec.Code, rec.Body.String())
	}

	// A guest who owns no seat on the match must never get a secret.
	if rec := postSeatSecret("guest_stranger", "test-internal-token"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a guest with no seat, got %d body=%s", rec.Code, rec.Body.String())
	}

	// A seated guest queried by a trusted caller gets exactly their seat's
	// secret and nothing else.
	rec := postSeatSecret("guest_black_queued", "test-internal-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with service token, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected seat-secret payload to decode, got %v", err)
	}
	if payload.Secret != "server-generated-black-secret" {
		t.Fatalf("expected the black seat's real secret, got %q", payload.Secret)
	}

	// An unknown match must 404, not leak anything.
	req := httptest.NewRequest(http.MethodPost, "/api/matches/room_unknown/seat-secret", strings.NewReader(`{"guestId":"guest_white_queued"}`))
	req.Header.Set("X-Chess404-Service-Token", "test-internal-token")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown match, got %d", rec2.Code)
	}
}

// TestSeatSecretEndpointRejectsComputerMatches pins the rule that the engine
// seat of a computer match is never claimable: only human seats resolve.
func TestSeatSecretEndpointRejectsComputerMatches(t *testing.T) {
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = archive.Close() }()

	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")

	service := match.NewService()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:      "seat_secret_computer_test",
		Queue:        "computer",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guest_human",
	}, now)

	mux := buildMatchServiceMux(service, archive, websocket.Upgrader{}, 64*1024)

	for _, guestID := range []string{"guest_human", "guest_engine"} {
		body := fmt.Sprintf(`{"guestId":%q}`, guestID)
		req := httptest.NewRequest(http.MethodPost, "/api/matches/seat_secret_computer_test/seat-secret", strings.NewReader(body))
		req.Header.Set("X-Chess404-Service-Token", "test-internal-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("expected computer match seat-secret to be refused for guest %q, got 200 body=%s", guestID, rec.Body.String())
		}
	}
}

// TestApplyIntentHTTPResponseHidesOpponentHand is a regression test for a
// live production bug affecting every move of every match: ApplyIntent's
// raw response is the same internal, full-visibility snapshot JoinMatchSeat
// returns (both hands populated), and the POST .../intents handler only
// redacted seat secrets before writing it -- so the mover's own move response
// carried their opponent's full hand back to them on every single move, in
// both Open Cards and (defeating its entire purpose) Hidden Cards mode. The
// ongoing WebSocket broadcast stream already scoped each subscriber's copy
// correctly via filterStateForColor; this handler just never got the same
// treatment.
func TestApplyIntentHTTPResponseHidesOpponentHand(t *testing.T) {
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = archive.Close() }()

	service := match.NewService()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "intent_hand_leak_test",
		Queue:             "casual",
		WhiteGuestID:      "guest_white_mover",
		WhitePlayerSecret: "white-movers-secret",
		BlackGuestID:      "guest_black_opponent",
		BlackPlayerSecret: "black-opponents-secret",
	}, now)

	mux := buildMatchServiceMux(service, archive, websocket.Upgrader{}, 64*1024)

	body := `{"intent":{"type":"make_move","playerId":"guest_white_mover","playerSecret":"white-movers-secret","from":{"row":1,"col":4},"to":{"row":3,"col":4}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/matches/intent_hand_leak_test/intents", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected move to succeed, got status %d body=%s", rec.Code, rec.Body.String())
	}

	var resp contracts.MatchSnapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected intent response to decode, got %v", err)
	}
	if len(resp.Match.BlackHand) != 0 {
		t.Fatalf("expected the mover's (white) response to hide the opponent's (black) hand, got %d black cards: %+v",
			len(resp.Match.BlackHand), resp.Match.BlackHand)
	}
	if len(resp.Match.WhiteHand) == 0 {
		t.Fatal("expected the mover's own (white) hand to still be visible in their own move response")
	}
	if resp.Match.WhitePlayerSecret != "" || resp.Match.BlackPlayerSecret != "" {
		t.Fatalf("expected both seat secrets to remain redacted, got white=%q black=%q",
			resp.Match.WhitePlayerSecret, resp.Match.BlackPlayerSecret)
	}
}

func TestWithCORSRejectsEmptyAllowlist(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")

	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/matches/room_test", nil)
	req.Header.Set("Origin", "https://anywhere.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("empty allowlist should not set Allow-Origin (rejected for security), got Allow-Origin=%q", got)
	}
}

// Regression test for a live production outage: the Railway variable
// PLATFORM_SERVICE_INTERNAL_URL is stored as
// "http://platform-service.railway.internal:" (trailing colon, no port). The
// WS claim-resolution path dialed that URL on port 80 and failed on every
// WebSocket auth frame ("auth.error unauthorized"), while the HTTP intent path
// kept working because the gateway resolves the same variable correctly.
func TestPlatformServiceURLNormalizesTrailingColon(t *testing.T) {
	t.Setenv("PLATFORM_SERVICE_INTERNAL_URL", "http://platform-service.railway.internal:")
	if got := platformServiceURL(); got != "http://platform-service.railway.internal:8080" {
		t.Fatalf("expected trailing-colon URL to get the default port appended, got %q", got)
	}

	t.Setenv("PLATFORM_SERVICE_INTERNAL_URL", "http://platform-service.railway.internal:9090")
	if got := platformServiceURL(); got != "http://platform-service.railway.internal:9090" {
		t.Fatalf("expected an explicit port to be preserved, got %q", got)
	}

	t.Setenv("PLATFORM_SERVICE_INTERNAL_URL", "${{platform-service.RAILWAY_PRIVATE_DOMAIN}}")
	if got := platformServiceURL(); got != "http://platform-service:8080" {
		t.Fatalf("expected an unresolved Railway template to fall back to the default, got %q", got)
	}

	t.Setenv("PLATFORM_SERVICE_INTERNAL_URL", "")
	if got := platformServiceURL(); got != "http://platform-service:8080" {
		t.Fatalf("expected an unset variable to fall back to the default, got %q", got)
	}
}
