package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestIntentWithStaleSeqNumAfterOpponentMoveIsAccepted is a regression test
// for a live vs-computer bug: the human's move response carries seq N+1, the
// server immediately auto-plays the computer (seq N+2), and the human's next
// click -- racing the WS delivery of the computer's move -- still carried
// N+1. ApplyIntent rejected it with ErrStaleClientState (a 409 the browser
// surfaced as repeated ".../intents 409" console errors), the client resynced
// in the background, and the player had to click again. The staleness check
// was a fail-fast optimization, not a correctness gate: every intent is fully
// revalidated against the CURRENT state (seat auth, rate limit, turn, move
// legality), and the ClientMoveID dedupe above it absorbs replays. A
// stale-but-legal move must be applied.
//
// A fast human opponent produces the identical race, so this test uses two
// human seats and simulates the opponent's reply landing between the first
// player's response and their next submission.
func TestIntentWithStaleSeqNumAfterOpponentMoveIsAccepted(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "stale_seq_race",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	// White: e2-e4 (seq N+1). The seq the client tracks is the one the WS
	// broadcast minted (the HTTP response itself is seq-less on this path).
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "stale_seq_race",
		PlayerID: "guest-white",
		From:     &contracts.Square{Row: 1, Col: 4},
		To:       &contracts.Square{Row: 3, Col: 4},
	}, now); err != nil {
		t.Fatalf("expected white's opening move to succeed, got %v", err)
	}
	seqAfterWhiteMove := containerSeqNum(t, service, "stale_seq_race")

	// Black replies immediately (seq N+2) -- the fast opponent whose broadcast
	// has not reached the client yet.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "stale_seq_race",
		PlayerID: "guest-black",
		From:     &contracts.Square{Row: 6, Col: 4},
		To:       &contracts.Square{Row: 4, Col: 4},
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected black's reply to succeed, got %v", err)
	}
	seqAfterBlackMove := containerSeqNum(t, service, "stale_seq_race")
	if seqAfterBlackMove <= seqAfterWhiteMove {
		t.Fatalf("expected black's move to advance the seq (got %d <= %d)", seqAfterBlackMove, seqAfterWhiteMove)
	}

	// White's next click, built against the pre-reply seq (stale by exactly
	// the opponent's move): g1-f3, legal in the current position.
	respThird, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:           "make_move",
		MatchID:        "stale_seq_race",
		PlayerID:       "guest-white",
		ExpectedSeqNum: seqAfterWhiteMove,
		From:           &contracts.Square{Row: 0, Col: 6},
		To:             &contracts.Square{Row: 2, Col: 5},
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("expected a stale-but-legal move to be applied, got %v", err)
	}
	if respThird.Match.Turn != "black" {
		t.Fatalf("expected turn to hand back to black, got %q", respThird.Match.Turn)
	}
	if seqAfterThird := containerSeqNum(t, service, "stale_seq_race"); seqAfterThird <= seqAfterBlackMove {
		t.Fatalf("expected the applied move to mint a fresh seq (got %d <= %d)", seqAfterThird, seqAfterBlackMove)
	}

	// Stale AND illegal must still fail -- with the specific rule error from
	// full revalidation, not a blanket staleness rejection. White's pawn on
	// e4 cannot capture the empty d5 square.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:           "make_move",
		MatchID:        "stale_seq_race",
		PlayerID:       "guest-white",
		ExpectedSeqNum: seqAfterWhiteMove,
		From:           &contracts.Square{Row: 3, Col: 4},
		To:             &contracts.Square{Row: 2, Col: 3},
	}, now.Add(3*time.Second)); err == nil {
		t.Fatalf("expected a stale-and-illegal move to be rejected")
	}
}

func containerSeqNum(t *testing.T, service *Service, matchID string) int64 {
	t.Helper()
	c := service.getMatchContainer(matchID)
	if c == nil {
		t.Fatalf("expected container for %s", matchID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seqNum
}
