package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestCloneCannotExposeTheEnemyKing was written to test a hypothesized P0:
// that "clone" resolution (match_cards.go's "clone" case) might not call
// kingsRemainSafe after creating the new piece, the way
// fusion/mindcontrol/borrow/teleport/jump/swap* all do -- which would let a
// clone open a fresh attack on the enemy king, followed by a same-turn move
// (clone doesn't end the turn) capturing it outright, with
// evaluateAutomaticMatchFinish never noticing (gameStatusWithFusion returns
// false,false,false when findKing finds no king -- confirmed by reading
// chess.go:154-163 -- so the match would stay "active" forever with a
// missing king).
//
// The hypothesis was WRONG: clone.go:788 does call kingsRemainSafe, exactly
// like every other board-mutating mechanic (confirmed by grep: 17 call
// sites across match_cards.go, one per mechanic that could plausibly expose
// a king). This test is kept as a permanent regression guard for that
// safeguard specifically, since the failure mode if it ever regressed would
// be severe and silent.
func TestCloneCannotExposeTheEnemyKing(t *testing.T) {
	service := NewService()
	defer service.Close()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "king_capture_poc"}, now)

	state := service.getMatchContainer("king_capture_poc").state
	state.Board = emptyBoard()
	state.Board[0][7] = &contracts.Piece{Type: "king", Color: "white"} // h1, uninvolved
	state.Board[3][0] = &contracts.Piece{Type: "rook", Color: "white"} // a4 -- no line to b8
	state.Board[7][1] = &contracts.Piece{Type: "king", Color: "black"} // b8

	cloneCardID := cardIDByMechanic(t, snapshot.Match.WhiteHand, "clone")

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "king_capture_poc", PlayerID: "white_player", CardID: cloneCardID,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected clone play_card to succeed, got %v", err)
	}
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "select_target", MatchID: "king_capture_poc", PlayerID: "white_player",
		Target: &contracts.Square{Row: 3, Col: 0}, // a4, the source rook
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected clone source selection to succeed, got %v", err)
	}
	// b4 (row 3, col 1): adjacent to a4, and on the SAME FILE as b8 -- would
	// be a brand-new rook line onto the black king that did not exist
	// before. kingsRemainSafe must reject this.
	_, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "select_target", MatchID: "king_capture_poc", PlayerID: "white_player",
		Target: &contracts.Square{Row: 3, Col: 1},
	}, now.Add(3*time.Second))
	if err == nil {
		t.Fatal("CONFIRMED BUG: clone allowed a destination that exposes the enemy king to a fresh attack -- kingsRemainSafe regressed")
	}
	if err.Error() != "clone destination is not safe" {
		t.Fatalf("expected rejection to be kingsRemainSafe's \"clone destination is not safe\", got a different error (mismatched test setup, not necessarily a bug): %v", err)
	}

	// Confirm the match is otherwise still healthy: the rejected select_target
	// must not have partially mutated anything -- the pending clone is still
	// waiting on a (different, safe) destination, and both kings remain on
	// the board.
	live, err := service.GetMatch("king_capture_poc")
	if err != nil {
		t.Fatalf("expected match to still be queryable, got %v", err)
	}
	if live.Match.PendingCard == nil || live.Match.PendingCard.Mechanic != "clone" {
		t.Fatalf("expected the clone card to still be pending its destination after a rejected attempt, got %#v", live.Match.PendingCard)
	}
	if bk := live.Match.Board[7][1]; bk == nil || bk.Type != "king" || bk.Color != "black" {
		t.Fatalf("expected the black king to remain untouched at b8, got %#v", bk)
	}
}
