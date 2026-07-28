package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression test for a bug where every "Play vs Computer" match was
// completely unplayable in production. CreateMatchRequest for a computer
// match only ever carries WhiteGuestID (the client has no black guest to
// send -- the computer seat is assigned server-side), so the
// hasWhiteSeat-vs-hasBlackSeat check landed the match on status "waiting".
// The computer-mode block then set BlackGuestID = "computer" but never
// flipped status back to "active". Every intent handler (applyMove,
// applyPlayCard, applyResign, ...) calls ensureActive and rejects anything
// but "active", so the first move a human made against the computer failed
// with "match is not active" -- and the computer's own auto-move logic has
// the identical guard, so it could never move either.
func TestComputerMatchStartsActive(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	created := service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "computer_active",
		ModeID:            contracts.MatchModeComputer,
		Difficulty:        "medium",
		WhiteGuestID:      "guest_white",
		WhitePlayerSecret: "white-secret",
	}, now)

	if created.Match.Status != "active" {
		t.Fatalf("expected a computer match to start active, got status=%q", created.Match.Status)
	}
	if created.Match.BlackGuestID != "computer" {
		t.Fatalf("expected the black seat to be assigned to the computer, got %q", created.Match.BlackGuestID)
	}
}

// The status bug meant this exact call sequence -- the one a real browser
// makes on the first move of a computer game -- returned "match is not
// active" for every player, every game, unconditionally.
func TestComputerMatchAcceptsFirstMove(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "computer_move",
		ModeID:            contracts.MatchModeComputer,
		Difficulty:        "medium",
		WhiteGuestID:      "guest_white",
		WhitePlayerSecret: "white-secret",
	}, now)

	resp, err := service.ApplyIntent(contracts.PlayerIntent{
		Type:         "make_move",
		MatchID:      "computer_move",
		PlayerID:     "guest_white",
		PlayerSecret: "white-secret",
		From:         &contracts.Square{Row: 1, Col: 1},
		To:           &contracts.Square{Row: 3, Col: 1},
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("expected the opening move to be accepted, got error: %v", err)
	}
	if resp.Match.Status != "active" {
		t.Fatalf("expected the match to remain active after a move, got status=%q", resp.Match.Status)
	}
}
