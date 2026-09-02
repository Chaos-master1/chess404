package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestFusedRookMakingAKnightShapedMoveDoesNotHang is a regression test for a
// production availability bug found by xgauntlet's E0 cross-engine gauntlet:
// a rook fused with a knight (FusedWith keeps the survivor's OWN Type
// unchanged -- see applyFusion / TestFullFusionSelectsTwoPiecesAndAppliesFusion)
// making its fusion-granted knight-shaped move hung applyMove forever.
//
// Root cause: applyMove's fortress-path check (match_actions.go, just after
// the legalMovesWithFusion check) gated on isSlider(piece.Type) alone --
// "rook" for a rook+knight fusion survivor, regardless of which movement
// pattern the ACTUAL move used. pathCrossesFortress then steps from From
// toward To one square at a time along sign(dRow)/sign(dCol), assuming a
// straight line or diagonal; a genuine knight-shaped delta (e.g. (+2,+1))
// never satisfies that stepping pattern, so the loop never terminates -- and
// since ApplyIntent holds the match's own mutex for the call's duration,
// this permanently hangs every future operation on that match. This is not
// exploitable only via card-search accidents: any human player fusing a
// rook/bishop/queen with a knight and then making the knight-shaped move
// could trigger the identical hang in production.
//
// The fix requires the move to actually be a straight line or diagonal
// (dRow==0 || dCol==0 || abs(dRow)==abs(dCol)) before treating it as a
// slider path to check -- correct, not just crash-avoidance, since a
// knight-shaped hop has no intermediate squares to check for fortress-
// crossing in the first place.
//
// Run in a goroutine with its own timeout rather than relying on `go test`'s
// package-wide timeout: this bug's failure mode IS a hang, so a regression
// should fail this one test quickly and clearly instead of stalling the
// whole suite for however many minutes the outer timeout allows.
func TestFusedRookMakingAKnightShapedMoveDoesNotHang(t *testing.T) {
	service := NewService()
	defer service.Close()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "fusion_knight_move"}, now)
	cardID := cardIDByMechanic(t, snapshot.Match.WhiteHand, "fullfusion")

	state := service.getMatchContainer("fusion_knight_move").state
	state.Board = emptyBoard()
	state.Board[0][0] = &contracts.Piece{Type: "king", Color: "white"}   // a1
	state.Board[7][7] = &contracts.Piece{Type: "king", Color: "black"}   // h8
	state.Board[3][3] = &contracts.Piece{Type: "knight", Color: "white"} // d4, consumed
	state.Board[3][4] = &contracts.Piece{Type: "rook", Color: "white"}   // e4, survives, fused
	// A real fortress zone present too, covering f5 (row4,col5) -- the
	// square the OLD, buggy diagonal-stepping code would have walked
	// through as its first (fake) step from e4 toward f6 -- but NOT f6
	// itself, so this proves the fix causes pathCrossesFortress to be
	// correctly SKIPPED for a non-straight-line move, rather than merely
	// having nothing to check.
	state.FortressZones = []contracts.FortressZone{
		{OwnerColor: "black", TopRow: 3, LeftCol: 5, TurnsLeft: 5},
	}

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "fusion_knight_move", PlayerID: "white_player", CardID: cardID,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected fullfusion play_card to succeed, got %v", err)
	}
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "select_target", MatchID: "fusion_knight_move", PlayerID: "white_player",
		Target: &contracts.Square{Row: 3, Col: 3}, // d4 knight, consumed (First)
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected fullfusion first selection to succeed, got %v", err)
	}
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "select_target", MatchID: "fusion_knight_move", PlayerID: "white_player",
		Target: &contracts.Square{Row: 3, Col: 4}, // e4 rook, survives (Second)
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("expected fullfusion second selection to succeed, got %v", err)
	}

	fused := service.getMatchContainer("fusion_knight_move").state.Board[3][4]
	if fused == nil || fused.Type != "rook" || fused.FusedWith != "knight" {
		t.Fatalf("test setup error: expected a rook fused with knight at e4, got %#v", fused)
	}

	// e4 (row3,col4) -> f6 (row5,col5): delta (+2,+1), a knight shape, not a
	// straight line or diagonal for a rook.
	from := contracts.Square{Row: 3, Col: 4}
	to := contracts.Square{Row: 5, Col: 5}

	done := make(chan error, 1)
	go func() {
		_, err := applyTestIntent(service, contracts.PlayerIntent{
			Type: "make_move", MatchID: "fusion_knight_move", PlayerID: "white_player",
			From: &from, To: &to,
		}, now.Add(4*time.Second))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected the fused rook's knight-shaped move to succeed, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("applyMove hung on a fused piece's non-straight-line move -- the fortress-path geometry guard regressed")
	}

	moved := service.getMatchContainer("fusion_knight_move").state.Board[5][5]
	if moved == nil || moved.Type != "rook" || moved.FusedWith != "knight" {
		t.Fatalf("expected the fused rook to have landed on f6, got %#v", moved)
	}
}
