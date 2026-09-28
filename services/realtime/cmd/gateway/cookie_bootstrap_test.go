package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSessionCookies(t *testing.T) {
	cookies := parseSessionCookies("session_guest_white=guest-w; session_secret_white=sec-w; other=1; session_secret_black=sec%2Db; session_secret_white=")
	if cookies["white"].GuestID != "guest-w" {
		t.Fatalf("white guest id = %q, want guest-w", cookies["white"].GuestID)
	}
	if cookies["white"].SessionSecret != "sec-w" {
		t.Fatalf("white secret = %q, want sec-w", cookies["white"].SessionSecret)
	}
	if cookies["black"].SessionSecret != "sec-b" {
		t.Fatalf("black secret = %q, want sec-b (percent-decoded)", cookies["black"].SessionSecret)
	}
	if cookies["black"].GuestID != "" {
		t.Fatalf("black guest id = %q, want empty", cookies["black"].GuestID)
	}
	if !(sessionCookieSide{}).empty() {
		t.Fatal("zero-value cookie side must be empty")
	}
}

func TestFoldSessionCookieIdentitiesFillsUnsuppliedSides(t *testing.T) {
	request := GatewayBootstrapRequest{}
	req := httptest.NewRequest(http.MethodPost, "/api/session/bootstrap", nil)
	req.Header.Set("Cookie", "session_guest_white=guest-w; session_secret_white=sec-w; session_guest_black=guest-b; session_secret_black=sec-b")

	folded := foldSessionCookieIdentities(request, req)
	if folded.White == nil || folded.White.GuestID != "guest-w" || folded.White.SessionSecret != "sec-w" {
		t.Fatalf("white identity = %+v, want cookie-folded credentials", folded.White)
	}
	if folded.Black == nil || folded.Black.GuestID != "guest-b" || folded.Black.SessionSecret != "sec-b" {
		t.Fatalf("black identity = %+v, want cookie-folded credentials", folded.Black)
	}
}

func TestFoldSessionCookieIdentitiesJSONCredentialsWin(t *testing.T) {
	request := GatewayBootstrapRequest{
		White: &GatewayGuestIdentity{GuestID: "json-guest", SessionSecret: "json-secret"},
		Black: &GatewayGuestIdentity{GuestID: "json-guest-b"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/session/bootstrap", nil)
	req.Header.Set("Cookie", "session_guest_white=cookie-w; session_secret_white=cookie-secret; session_secret_black=cookie-secret-b")

	folded := foldSessionCookieIdentities(request, req)
	if folded.White.SessionSecret != "json-secret" || folded.White.GuestID != "json-guest" {
		t.Fatalf("white identity = %+v, want JSON credentials untouched", folded.White)
	}
	if folded.Black.GuestID != "json-guest-b" {
		t.Fatalf("black guest id = %q, want the JSON value kept", folded.Black.GuestID)
	}
	if folded.Black.SessionSecret != "cookie-secret-b" {
		t.Fatalf("black secret = %q, want the cookie value folded in", folded.Black.SessionSecret)
	}
}

func TestFoldSessionCookieIdentitiesNoCookiesIsNoop(t *testing.T) {
	white := &GatewayGuestIdentity{GuestID: "g1"}
	request := GatewayBootstrapRequest{White: white}
	req := httptest.NewRequest(http.MethodPost, "/api/session/bootstrap", nil)

	folded := foldSessionCookieIdentities(request, req)
	if folded.White != white {
		t.Fatal("expected the original identity pointer to be preserved")
	}
	if folded.White.SessionSecret != "" {
		t.Fatal("expected no secret to appear without cookies")
	}
}

// End to end: a browser that lost localStorage but kept its HttpOnly cookies
// must resume its session (not mint a fresh guest), receive the resumed
// identity, and get fresh Set-Cookie headers back.
func TestBootstrapResumesSessionFromCookiesOnly(t *testing.T) {
	resumes := map[string]string{} // supplied secret -> resumed guest id
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/guest-sessions":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			resumes[payload["sessionSecret"]] = payload["guestId"]
			_ = json.NewEncoder(w).Encode(map[string]any{
				"guest": map[string]any{
					"guestId":     payload["guestId"],
					"displayName": "Guest " + payload["guestId"],
					"rating":      1200,
				},
				"sessionSecret": payload["sessionSecret"],
			})
		case "/api/platform/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer platformServer.Close()

	t.Setenv("PLATFORM_SERVICE_INTERNAL_URL", platformServer.URL)
	t.Setenv("MATCH_SERVICE_INTERNAL_URL", "http://match-service:8080")
	t.Setenv("MATCHMAKING_SERVICE_INTERNAL_URL", "http://matchmaking-service:8080")
	t.Setenv("ALLOWED_ORIGINS", "https://web.test")

	config := gatewayConfigFromEnv()
	mux := buildGatewayMux(config, &http.Client{})

	req := httptest.NewRequest(http.MethodPost, "/api/session/bootstrap", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://web.test")
	req.Header.Set("Cookie", "session_guest_white=guest-w; session_secret_white=sec-w")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if resumes["sec-w"] != "guest-w" {
		t.Fatalf("cookie identity was not resumed via guest-sessions (got %v); body=%s", resumes, rec.Body.String())
	}

	var envelope struct {
		Payload struct {
			GuestSessions *struct {
				White *struct {
					Guest struct {
						GuestID string `json:"guestId"`
					} `json:"guest"`
					SessionSecret string `json:"sessionSecret"`
				} `json:"white"`
			} `json:"guestSessions"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode bootstrap response: %v", err)
	}
	white := envelope.Payload.GuestSessions.White
	if white == nil || white.Guest.GuestID != "guest-w" {
		t.Fatalf("bootstrap returned white=%+v, want the resumed cookie guest", white)
	}
	if white.SessionSecret != "" {
		t.Fatal("resumed-supplied guest secret must be stripped from the JSON (it went out via Set-Cookie)")
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "session_secret_white=sec-w") {
		t.Fatalf("expected the session cookie to be re-minted, got Set-Cookie: %q", rec.Header().Get("Set-Cookie"))
	}
}
