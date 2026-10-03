package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/platform"
)

// A finished match used to be unreadable to the people who played it.
//
// The claims route gated on a LIVENESS predicate (waiting/active only), so
// every claim against a completed match 404'd. The web layer therefore had no
// verified seat to scope the snapshot with, fell through to its public
// spectator gate -- which requires status == "active" -- and answered 404
// "match is not public" for BOTH players of every finished game, while
// match-service served the very same match with 200. Observed live: a
// completed match drew 200 from match-service and 404 from web, with a client
// polling it for 34 minutes after it ended.
//
// These tests pin the separation: ownership grants READ for a finished match,
// aborted/unknown stay refused, and writes remain blocked independently by
// ensureActive() in the match engine.

func newFinishedMatchArchive(t *testing.T) (*platform.MatchArchiveStore, *platform.GuestStore, *platform.MatchClaimStore, string, string) {
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

	claims := platform.NewMatchClaimStore()

	session, err := guests.EnsureGuest("guest_white", "")
	if err != nil {
		t.Fatalf("expected guest session creation to succeed, got %v", err)
	}

	now := time.Date(2026, 10, 2, 21, 8, 52, 0, time.UTC)
	if err := archive.Upsert(contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:           "room_finished",
			RulesVersion:      "v1-alpha-foundation",
			Queue:             "direct",
			ModeID:            "computer",
			WhiteGuestID:      session.Guest.GuestID,
			BlackGuestID:      "computer",
			WhiteName:         session.Guest.DisplayName,
			BlackName:         "Computer Medium",
			WhitePlayerSecret: "room_secret_white",
			Status:            "finished",
			Winner:            "white",
			FinishReason:      "checkmate",
			CreatedAt:         now,
			UpdatedAt:         now.Add(3 * time.Minute),
		},
	}); err != nil {
		t.Fatalf("expected archive upsert to succeed, got %v", err)
	}

	return archive, guests, claims, session.Guest.GuestID, session.SessionSecret
}

// postFinishedMatchClaim drives a claim request through a freshly built mux.
// It wraps the package's existing postMatchClaim helper rather than
// duplicating the request shape.
func postFinishedMatchClaim(t *testing.T, archive *platform.MatchArchiveStore, guests platform.GuestDirectory, claims *platform.MatchClaimStore, matchID, guestID, sessionSecret string) *httptest.ResponseRecorder {
	t.Helper()
	return postMatchClaim(buildTestPlatformMux(t, archive, guests, claims), matchID, guestID, sessionSecret)
}

func TestMatchClaimAllowsSeatOwnerToReadFinishedMatch(t *testing.T) {
	archive, guests, claims, guestID, sessionSecret := newFinishedMatchArchive(t)

	rec := postFinishedMatchClaim(t, archive, guests, claims, "room_finished", guestID, sessionSecret)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected a seat owner to claim a finished match, got status %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		MatchID      string `json:"matchId"`
		SeatColor    string `json:"seatColor"`
		PlayerID     string `json:"playerId"`
		PlayerSecret string `json:"playerSecret"`
		ClaimToken   string `json:"claimToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected claim response to decode, got %v", err)
	}
	if response.MatchID != "room_finished" {
		t.Fatalf("expected the claim to name the requested match, got %q", response.MatchID)
	}
	if response.SeatColor != "white" {
		t.Fatalf("expected the owning seat colour, got %q", response.SeatColor)
	}
	if response.PlayerID != guestID {
		t.Fatalf("expected playerId %q, got %q", guestID, response.PlayerID)
	}
	if response.PlayerSecret != "room_secret_white" {
		t.Fatalf("expected the archived seat secret to be issued, got %q", response.PlayerSecret)
	}
	if response.ClaimToken == "" {
		t.Fatal("expected a claim token in the issued claim")
	}
}

func TestMatchClaimStillRefusesAbortedMatch(t *testing.T) {
	tempDir := t.TempDir()
	archive, err := platform.NewMatchArchiveStore(filepath.Join(tempDir, "archive.json"))
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = archive.Close() }()
	guests, err := platform.NewGuestStore(filepath.Join(tempDir, "guests.json"))
	if err != nil {
		t.Fatalf("expected guest store to initialize, got %v", err)
	}
	defer func() { _ = guests.Close() }()

	claims := platform.NewMatchClaimStore()
	session, err := guests.EnsureGuest("guest_white", "")
	if err != nil {
		t.Fatalf("expected guest session creation to succeed, got %v", err)
	}

	now := time.Date(2026, 10, 2, 21, 8, 52, 0, time.UTC)
	// Upsert DELETES an aborted row rather than storing it (history.go), so
	// this request finds no archive row and 404s as "unknown match archive".
	// The point of the case is that widening finished-match reads did not
	// widen aborted-match reads: either way the answer stays 404.
	if err := archive.Upsert(contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      "room_aborted",
			RulesVersion: "v1-alpha-foundation",
			WhiteGuestID: session.Guest.GuestID,
			BlackGuestID: "computer",
			Status:       "aborted",
			Winner:       "aborted",
			FinishReason: "abort",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}); err != nil {
		t.Fatalf("expected archive upsert to succeed, got %v", err)
	}

	rec := postFinishedMatchClaim(t, archive, guests, claims, "room_aborted", session.Guest.GuestID, session.SessionSecret)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected an aborted match to stay unclaimable, got status %d body=%s", rec.Code, rec.Body.String())
	}
}

// The status check no longer refuses a finished match, so seat ownership is the
// ONLY thing standing between a stranger and its final state. That gate needs
// its own proof.
func TestMatchClaimRefusesFinishedMatchToNonParticipant(t *testing.T) {
	archive, guests, claims, _, _ := newFinishedMatchArchive(t)

	stranger, err := guests.EnsureGuest("guest_stranger", "")
	if err != nil {
		t.Fatalf("expected stranger session creation to succeed, got %v", err)
	}

	rec := postFinishedMatchClaim(t, archive, guests, claims, "room_finished", stranger.Guest.GuestID, stranger.SessionSecret)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected a non-participant to be refused, got status %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := claims.Get("room_finished", stranger.Guest.GuestID); ok {
		t.Fatal("expected no claim to be issued to a non-participant")
	}
}

func TestIsReadableMatchStatus(t *testing.T) {
	for _, status := range []string{"waiting", "active", "finished", "FINISHED", " finished "} {
		if !isReadableMatchStatus(status) {
			t.Fatalf("expected %q to be readable by a proven seat owner", status)
		}
	}
	for _, status := range []string{"aborted", "", "unknown", "cancelled"} {
		if isReadableMatchStatus(status) {
			t.Fatalf("expected %q to stay unreadable", status)
		}
	}
}
