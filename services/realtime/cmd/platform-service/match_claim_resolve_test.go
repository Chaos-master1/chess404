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

// Regression tests for the claim-token lifecycle. The resolve endpoint used to
// delete the claim token BEFORE the fallible archive refresh, so one transient
// archive outage burned the token and the seat 404'd forever ("unknown room
// claim token" on every retry). The fix: peek first, refresh, and only treat
// "archive has no recoverable row" as permanent when the archive backend is
// not degraded; success renews the leased token as before.

func newClaimResolveTestEnv(t *testing.T) (*platform.MatchArchiveStore, *platform.GuestStore, *platform.MatchClaimStore, string) {
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

	session, err := guests.EnsureGuest("guest_claim_resolve", "")
	if err != nil {
		t.Fatalf("expected guest session creation to succeed, got %v", err)
	}
	return archive, guests, claims, session.Guest.GuestID
}

func postResolve(t *testing.T, mux http.Handler, matchID, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/platform/match-claims/resolve",
		strings.NewReader(`{"matchId":"`+matchID+`","claimToken":"`+token+`"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// A transient archive outage must NOT burn the token: the resolve fails, but
// the claim survives so the seat can retry once the archive is healthy again.
func TestMatchClaimResolveKeepsClaimDuringArchiveOutage(t *testing.T) {
	archive, guests, claims, guestID := newClaimResolveTestEnv(t)
	// No archive row: LoadMatch misses. With the outage flag set, that miss
	// must be treated as transient rather than permanent.
	_ = archive

	if err := claims.Put(platform.MatchSeatClaim{
		MatchID:      "room_outage",
		GuestID:      guestID,
		SeatColor:    "white",
		PlayerID:     guestID,
		PlayerSecret: "outage_secret",
	}); err != nil {
		t.Fatalf("expected claim put to succeed, got %v", err)
	}
	stored, ok := claims.Get("room_outage", guestID)
	if !ok || stored.ClaimToken == "" {
		t.Fatalf("expected a stored claim with a token, got ok=%v token=%q", ok, stored.ClaimToken)
	}

	previous := isLikelyArchiveOutage
	isLikelyArchiveOutage = func() bool { return true }
	defer func() { isLikelyArchiveOutage = previous }()

	mux := buildTestPlatformMux(t, archive, guests, claims)
	rec := postResolve(t, mux, "room_outage", stored.ClaimToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected resolve to fail during an outage, got status %d body=%s", rec.Code, rec.Body.String())
	}

	// The token must still be usable: the outage consumed nothing.
	if _, ok := claims.PeekByToken("room_outage", stored.ClaimToken); !ok {
		t.Fatal("transient archive outage must not consume the single-use claim token")
	}

	// Once the archive is healthy again and the row is still absent, the miss
	// is permanent and the dead claim is reaped.
	isLikelyArchiveOutage = func() bool { return false }
	rec = postResolve(t, mux, "room_outage", stored.ClaimToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected resolve against a permanently missing match to fail, got status %d", rec.Code)
	}
	if _, ok := claims.PeekByToken("room_outage", stored.ClaimToken); ok {
		t.Fatal("a permanently missing match must reap the dead claim")
	}
}

// On success the token is consumed exactly once (single-use preserved).
func TestMatchClaimResolveConsumesTokenOnlyOnSuccess(t *testing.T) {
	archive, guests, claims, guestID := newClaimResolveTestEnv(t)

	if err := archive.Upsert(contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:           "room_success",
			Status:            "active",
			Queue:             "rated",
			ModeID:            contracts.MatchModeOpenCards,
			WhiteGuestID:      guestID,
			WhitePlayerSecret: "success_secret",
			BlackGuestID:      "guest_other",
		},
	}); err != nil {
		t.Fatalf("expected archived match to persist, got %v", err)
	}
	if err := claims.Put(platform.MatchSeatClaim{
		MatchID:      "room_success",
		GuestID:      guestID,
		SeatColor:    "white",
		PlayerID:     guestID,
		PlayerSecret: "success_secret",
	}); err != nil {
		t.Fatalf("expected claim put to succeed, got %v", err)
	}
	stored, ok := claims.Get("room_success", guestID)
	if !ok || stored.ClaimToken == "" {
		t.Fatalf("expected a stored claim with a token, got ok=%v", ok)
	}

	mux := buildTestPlatformMux(t, archive, guests, claims)
	rec := postResolve(t, mux, "room_success", stored.ClaimToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected claim resolve to succeed, got status %d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		ClaimToken string `json:"claimToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected resolve response to decode, got %v", err)
	}
	if response.ClaimToken != stored.ClaimToken {
		t.Fatalf("expected the same token back, got %q want %q", response.ClaimToken, stored.ClaimToken)
	}

	// The token is a TTL-bounded lease: a successful resolve renews the
	// claim and the token stays usable (this is the client's reconnect
	// credential). What must never happen is a FAILURE burning it.
	if _, ok := claims.PeekByToken("room_success", stored.ClaimToken); !ok {
		t.Fatal("a successful resolve must keep the claim's token usable (lease semantics)")
	}
	rec = postResolve(t, mux, "room_success", stored.ClaimToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected a repeat resolve with the leased token to succeed, got status %d body=%s", rec.Code, rec.Body.String())
	}
}

// A genuinely dead claim (archived match finished) is still consumed on a
// resolve attempt -- the fix narrows transient handling, not cleanup.
func TestMatchClaimResolveReapsClaimForFinishedMatch(t *testing.T) {
	archive, guests, claims, guestID := newClaimResolveTestEnv(t)

	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if err := archive.Upsert(contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      "room_finished",
			Status:       "finished",
			Winner:       "white",
			FinishReason: "checkmate",
			Queue:        "rated",
			WhiteGuestID: guestID,
			BlackGuestID: "guest_other",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}); err != nil {
		t.Fatalf("expected archived match to persist, got %v", err)
	}
	if err := claims.Put(platform.MatchSeatClaim{
		MatchID:      "room_finished",
		GuestID:      guestID,
		SeatColor:    "white",
		PlayerID:     guestID,
		PlayerSecret: "finished_secret",
	}); err != nil {
		t.Fatalf("expected claim put to succeed, got %v", err)
	}
	stored, ok := claims.Get("room_finished", guestID)
	if !ok || stored.ClaimToken == "" {
		t.Fatalf("expected a stored claim with a token, got ok=%v", ok)
	}

	mux := buildTestPlatformMux(t, archive, guests, claims)
	rec := postResolve(t, mux, "room_finished", stored.ClaimToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected resolve for a finished match to fail, got status %d", rec.Code)
	}
	if _, ok := claims.PeekByToken("room_finished", stored.ClaimToken); ok {
		t.Fatal("a claim for a finished match must be reaped on resolve")
	}
}
