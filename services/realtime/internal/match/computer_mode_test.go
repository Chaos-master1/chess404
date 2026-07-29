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

// Regression test for a second, deeper bug in the same "Play vs Computer"
// path: even after the match started active, ComputerOpponent.MakeMove and
// HandleSelectTarget never populated PlayerID/PlayerSecret on the intents
// they returned -- they have no HTTP request to source an identity from --
// so requireIntentColor rejected every one of the computer's own moves as
// "unrecognized player id", forever, on every attempt, regardless of
// whether the computer chose a card or a normal move. The match would sit
// on "black to move" permanently after the human's first move. Separately,
// even once the computer's intents carry a valid identity, a card whose
// target the engine cannot resolve (HandleSelectTarget returns nil) used to
// leave PendingCard dangling forever with no fallback. This asserts the
// computer's reply always lands within a bounded, short time regardless.
func TestComputerRepliesToOpeningMove(t *testing.T) {
	service := NewService()
	now := time.Now()

	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "computer_replies",
		ModeID:            contracts.MatchModeComputer,
		Difficulty:        "medium",
		WhiteGuestID:      "guest_white",
		WhitePlayerSecret: "white-secret",
	}, now)

	if _, err := service.ApplyIntent(contracts.PlayerIntent{
		Type:         "make_move",
		MatchID:      "computer_replies",
		PlayerID:     "guest_white",
		PlayerSecret: "white-secret",
		From:         &contracts.Square{Row: 1, Col: 1},
		To:           &contracts.Square{Row: 3, Col: 1},
	}, now); err != nil {
		t.Fatalf("expected the opening move to be accepted, got error: %v", err)
	}

	// autoPlayComputer dispatches to a worker goroutine, so the computer's
	// reply is asynchronous -- poll briefly instead of asserting instantly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := service.GetMatchForViewer("computer_replies", "guest_white", "white-secret")
		if err != nil {
			t.Fatalf("GetMatchForViewer error: %v", err)
		}
		if snap.Match.Turn == "white" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("computer never replied to the opening move within 5s")
}
