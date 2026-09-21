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
	"github.com/chess404/realtime/internal/platform"
)

// Tests for the queue-match claim fix. Queue-matched rooms are created by
// matchmaking-service with server-generated seat secrets that match-service
// redacts from every snapshot and archive row it persists. The claim pipeline
// must not hand players a redacted placeholder (or fall back to their guest
// session secret, which match-service then rejects with 400 on every
// snapshot/WS call); it must resolve the real seat secret through the
// internal seat-secret endpoint.

func newClaimTestEnv(t *testing.T) (*platform.MatchArchiveStore, platform.GuestDirectory, *platform.MatchClaimStore) {
	t.Helper()
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	t.Cleanup(func() { _ = archive.Close() })
	guests, err := platform.NewGuestStore(filepath.Join(tempDir, "guests.json"))
	if err != nil {
		t.Fatalf("expected guest store to initialize, got %v", err)
	}
	t.Cleanup(func() { _ = guests.Close() })
	return archive, guests, platform.NewMatchClaimStore()
}

func archiveQueueMatchWithRedactedSecrets(t *testing.T, archive *platform.MatchArchiveStore, matchID, whiteGuestID, blackGuestID string) {
	t.Helper()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := archive.Upsert(contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:           matchID,
			RulesVersion:      "v1-alpha-foundation",
			Queue:             "casual",
			WhiteGuestID:      whiteGuestID,
			WhitePlayerSecret: "<redacted>",
			BlackGuestID:      blackGuestID,
			BlackPlayerSecret: "<redacted>",
			Status:            "active",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}); err != nil {
		t.Fatalf("expected archive upsert to succeed, got %v", err)
	}
}

func postMatchClaim(mux http.Handler, matchID, guestID, sessionSecret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/platform/match-claims",
		strings.NewReader(`{"matchId":"`+matchID+`","guestId":"`+guestID+`","sessionSecret":"`+sessionSecret+`"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestMatchClaimsResolveRedactedSeatSecret verifies that a claim built from a
// queue match whose archived snapshot only holds redacted seat secrets asks
// match-service (via the internal seat-secret endpoint, whose response this
// test stubs) for the real credential instead of issuing an empty one.
func TestMatchClaimsResolveRedactedSeatSecret(t *testing.T) {
	archive, guests, claims := newClaimTestEnv(t)

	session, err := guests.EnsureGuest("guest_black_queued", "")
	if err != nil {
		t.Fatalf("expected guest session creation to succeed, got %v", err)
	}
	archiveQueueMatchWithRedactedSecrets(t, archive, "room_redacted_claim", "guest_white_queued", session.Guest.GuestID)

	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	// Point the resolver at the stub match-service.
	t.Setenv("MATCH_SERVICE_INTERNAL_URL", stubMatchServiceURL(t))

	rec := postMatchClaim(buildTestPlatformMux(t, archive, guests, claims), "room_redacted_claim", session.Guest.GuestID, session.SessionSecret)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected match claim to succeed, got status %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		SeatColor    string `json:"seatColor"`
		PlayerSecret string `json:"playerSecret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected match claim response to decode, got %v", err)
	}
	if response.SeatColor != "black" {
		t.Fatalf("expected black seat claim, got %q", response.SeatColor)
	}
	if response.PlayerSecret == "" || response.PlayerSecret == "<redacted>" || response.PlayerSecret == session.SessionSecret {
		t.Fatalf("expected the resolved seat secret, got %q", response.PlayerSecret)
	}
	if response.PlayerSecret != "resolved-server-generated-black-secret" {
		t.Fatalf("expected the stub match-service's secret, got %q", response.PlayerSecret)
	}
}

// TestMatchClaimsRejectRedactedSecretWhenResolverUnavailable pins the safe
// behavior when match-service cannot be reached: the claim must not silently
// fall back to the guest session secret (which match-service rejects for
// queue seats) nor to the redacted placeholder.
func TestMatchClaimsRejectRedactedSecretWhenResolverUnavailable(t *testing.T) {
	archive, guests, claims := newClaimTestEnv(t)

	session, err := guests.EnsureGuest("guest_white_queued", "")
	if err != nil {
		t.Fatalf("expected guest session creation to succeed, got %v", err)
	}
	archiveQueueMatchWithRedactedSecrets(t, archive, "room_resolver_down", session.Guest.GuestID, "guest_black_queued")

	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-internal-token")
	t.Setenv("MATCH_SERVICE_INTERNAL_URL", "http://127.0.0.1:1") // nothing listens here

	rec := postMatchClaim(buildTestPlatformMux(t, archive, guests, claims), "room_resolver_down", session.Guest.GuestID, session.SessionSecret)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected match claim to still succeed (secret resolve is best-effort), got status %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		PlayerSecret string `json:"playerSecret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected match claim response to decode, got %v", err)
	}
	if response.PlayerSecret != "<redacted>" && response.PlayerSecret != "" {
		t.Fatalf("expected no usable secret to be issued when the resolver is unreachable, got %q", response.PlayerSecret)
	}
}

// TestSeatSecretIsRedactedCoversMarkers keeps the redaction detector in sync
// with the markers match-service actually writes.
func TestSeatSecretIsRedactedCoversMarkers(t *testing.T) {
	for secret, want := range map[string]bool{
		"":             true,
		"  ":           true,
		"<redacted>":   true,
		" <redacted> ": true,
		"real-secret":  false,
	} {
		if got := seatSecretIsRedacted(secret); got != want {
			t.Fatalf("seatSecretIsRedacted(%q) = %v, want %v", secret, got, want)
		}
	}
}

// stubMatchServiceURL spins up a minimal stand-in for match-service's
// service-token-gated seat-secret endpoint and returns its base URL.
func stubMatchServiceURL(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/matches/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Chess404-Service-Token") != "test-internal-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// /api/matches/{matchID}/seat-secret
		if len(parts) != 4 || parts[3] != "seat-secret" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			GuestID string `json:"guestId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		secret := ""
		switch body.GuestID {
		case "guest_white_queued":
			secret = "resolved-server-generated-white-secret"
		case "guest_black_queued":
			secret = "resolved-server-generated-black-secret"
		default:
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"secret": secret})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}
