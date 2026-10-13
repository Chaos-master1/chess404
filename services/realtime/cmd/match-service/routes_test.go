package main

import (
	"encoding/json"
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

func newRoutesTestService(t *testing.T, matchID string) (*match.Service, *http.ServeMux) {
	t.Helper()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(t.TempDir(), "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	t.Cleanup(func() { _ = archive.Close() })

	service := match.NewService()
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           matchID,
		Queue:             "casual",
		ModeID:            contracts.MatchModeHiddenCards,
		WhiteGuestID:      "guest_white_routes",
		WhitePlayerSecret: "white-routes-secret",
		BlackGuestID:      "guest_black_routes",
		BlackPlayerSecret: "black-routes-secret",
	}, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))

	return service, buildMatchServiceMux(service, archive, websocket.Upgrader{}, 64*1024, nil)
}

func TestSystemStatusRejectsNonGet(t *testing.T) {
	_, mux := newRoutesTestService(t, "status_method_test")

	req := httptest.NewRequest(http.MethodPost, "/api/system/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST /api/system/status, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/system/status", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/system/status, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected status payload to decode, got %v", err)
	}
	if payload.Status != "ok" || payload.Service != "match-service" {
		t.Fatalf("unexpected status payload: %+v", payload)
	}
}

func TestFinishedJobsEndpointTokenAuth(t *testing.T) {
	_, mux := newRoutesTestService(t, "finished_jobs_test")

	get := func(headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/matches/internal/finished-jobs?limit=5", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Method gate.
	methodReq := httptest.NewRequest(http.MethodPost, "/api/matches/internal/finished-jobs", nil)
	methodReq.Header.Set("X-Chess404-Service-Token", "test-internal-token")
	methodRec := httptest.NewRecorder()
	mux.ServeHTTP(methodRec, methodReq)
	if methodRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST finished-jobs, got %d", methodRec.Code)
	}

	// No token configured on the server: refuse loudly rather than authing
	// against an empty accept set.
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "")
	t.Setenv("CHESS404_INTERNAL_SERVICE_TOKEN", "")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "")
	if rec := get(nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with no internal token configured, got %d body=%s", rec.Code, rec.Body.String())
	}

	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	if rec := get(nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", rec.Code)
	}
	if rec := get(map[string]string{"X-Chess404-Service-Token": "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a wrong token, got %d", rec.Code)
	}

	// The Bearer Authorization path is what platform-service actually sends.
	rec := get(map[string]string{"Authorization": "Bearer test-internal-token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via Bearer token, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		MatchIDs []string `json:"matchIds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected finished-jobs payload to decode, got %v", err)
	}

	rec = get(map[string]string{"X-Chess404-Service-Token": "test-internal-token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via service-token header, got %d", rec.Code)
	}
}

func TestCreateMatchEndpointValidation(t *testing.T) {
	_, mux := newRoutesTestService(t, "create_validation_test")

	req := httptest.NewRequest(http.MethodGet, "/api/matches", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET /api/matches, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/matches", strings.NewReader("{not-json"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed create body, got %d body=%s", rec.Code, rec.Body.String())
	}

	body := `{"matchId":"create_via_http","queue":"direct","whiteGuestId":"guest_white_routes","whitePlayerSecret":"created-secret"}`
	req = httptest.NewRequest(http.MethodPost, "/api/matches", strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a valid create, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "created-secret") {
		t.Fatalf("create response echoed the seat secret back: %s", rec.Body.String())
	}
}

func TestViewerGETScopesResponse(t *testing.T) {
	service, mux := newRoutesTestService(t, "viewer_scope_test")

	// Spectator read: succeeds but must not leak either seat secret.
	req := httptest.NewRequest(http.MethodGet, "/api/matches/viewer_scope_test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a spectator viewer GET, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "white-routes-secret") || strings.Contains(rec.Body.String(), "black-routes-secret") {
		t.Fatalf("spectator viewer GET leaked a seat secret: %s", rec.Body.String())
	}

	// A claimed player with the WRONG secret is rejected outright, not
	// silently downgraded to spectator.
	req = httptest.NewRequest(http.MethodGet, "/api/matches/viewer_scope_test", nil)
	req.Header.Set("X-Player-ID", "guest_white_routes")
	req.Header.Set("X-Player-Secret", "not-the-secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a viewer GET with a wrong secret, got %d", rec.Code)
	}

	// With the right secret the player's own hand stays visible.
	req = httptest.NewRequest(http.MethodGet, "/api/matches/viewer_scope_test", nil)
	req.Header.Set("X-Player-ID", "guest_white_routes")
	req.Header.Set("X-Player-Secret", "white-routes-secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for the white seat's viewer GET, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contracts.MatchSnapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected viewer response to decode, got %v", err)
	}
	if len(resp.Match.WhiteHand) == 0 {
		t.Fatal("expected the white seat's own hand to be visible in their own viewer GET")
	}
	_ = service
}

func TestMatchSubrouteNotFound(t *testing.T) {
	_, mux := newRoutesTestService(t, "subroute_test")

	req := httptest.NewRequest(http.MethodGet, "/api/matches/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an empty match id, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/matches/subroute_test/bogus", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown subroute, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/matches/room_unknown", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown match, got %d", rec.Code)
	}
}

func TestTokenEndpointIssuesResolvableToken(t *testing.T) {
	service, mux := newRoutesTestService(t, "token_endpoint_test")

	post := func(url string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, url, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// No identity at all -> 400.
	if rec := post("/api/matches/token_endpoint_test/token", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without player identity, got %d", rec.Code)
	}

	// Header-based identity.
	rec := post("/api/matches/token_endpoint_test/token", map[string]string{
		"X-Player-ID":     "guest_white_routes",
		"X-Player-Secret": "white-routes-secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from the token endpoint, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected token payload to decode, got %v", err)
	}
	playerID, playerSecret, ok := service.ResolveAuthToken(payload.Token)
	if !ok || playerID != "guest_white_routes" || playerSecret != "white-routes-secret" {
		t.Fatalf("issued token did not resolve to the requesting seat: ok=%v id=%q", ok, playerID)
	}

	// Query-param identity fallback (the WS handshake URL form).
	rec = post("/api/matches/token_endpoint_test/token?i=guest_black_routes&s=black-routes-secret", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from the token endpoint via query params, got %d", rec.Code)
	}
}

func TestPresenceEndpointHeartbeatsAndRejects(t *testing.T) {
	_, mux := newRoutesTestService(t, "presence_test")

	post := func(url, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("/api/matches/presence_test/presence", "{bad"); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed presence body, got %d", rec.Code)
	}
	if rec := post("/api/matches/room_unknown/presence", `{"playerId":"guest_white_routes","playerSecret":"white-routes-secret"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for presence on an unknown match, got %d", rec.Code)
	}
	if rec := post("/api/matches/presence_test/presence", `{"playerId":"guest_white_routes","playerSecret":"wrong"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for presence with a wrong secret, got %d", rec.Code)
	}
	rec := post("/api/matches/presence_test/presence", `{"playerId":"guest_white_routes","playerSecret":"white-routes-secret"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for a valid presence heartbeat, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRedactToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/matches/x/token?i=p1&s=hunter2", "/api/matches/x/token?i=p1&s=[REDACTED]"},
		{"/api/matches/x/ws?token=abcdef", "/api/matches/x/ws?token=[REDACTED]"},
		{"/path?playerSecret=abc&other=keep", "/path?playerSecret=[REDACTED]&other=keep"},
		{"/path?secret=abc#frag", "/path?secret=[REDACTED]"},
		{"/path?notasecret=abc", "/path?notasecret=abc"},
		{"no query at all", "no query at all"},
	}
	for _, tc := range cases {
		if got := redactToken(tc.in); got != tc.want {
			t.Fatalf("redactToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestArchivePathEnvHelpers(t *testing.T) {
	t.Setenv("MATCH_ARCHIVE_PATH", "/custom/archive.json")
	if got := archivePath(); got != "/custom/archive.json" {
		t.Fatalf("archivePath() = %q, want /custom/archive.json", got)
	}
	t.Setenv("MATCH_ARCHIVE_PATH", "")
	if got := archivePath(); got != filepath.Join("data", "match-archive.json") {
		t.Fatalf("archivePath() default = %q", got)
	}

	t.Setenv("MATCH_ARCHIVE_SQLITE_PATH", "/custom/archive.sqlite")
	if got := archiveSQLitePath(); got != "/custom/archive.sqlite" {
		t.Fatalf("archiveSQLitePath() = %q, want /custom/archive.sqlite", got)
	}
	t.Setenv("MATCH_ARCHIVE_SQLITE_PATH", "")
	if got := archiveSQLitePath(); got != filepath.Join("data", "match-archive.sqlite") {
		t.Fatalf("archiveSQLitePath() default = %q", got)
	}

	t.Setenv("MATCH_ARCHIVE_POSTGRES_URL", "postgres://example")
	if got := archivePostgresURL(); got != "postgres://example" {
		t.Fatalf("archivePostgresURL() = %q", got)
	}
}
