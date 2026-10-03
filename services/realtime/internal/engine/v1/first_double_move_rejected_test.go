package v1

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

// firstDoubleMoveBoard builds the shared parasite-discovery position: black
// to move with DoubleMove MovesLeft==2, black's only legal move being the
// capture of white queen (5,3) whose parasite side effect destroys the
// lone blocker (black pawn (0,4)) on black rook (0,0)'s line to white
// king (0,7).
func firstDoubleMoveBoard() *contracts.MatchState {
	state := &contracts.MatchState{
		MatchID:     "predicate_board",
		Status:      "active",
		Turn:        "black",
		FullMoveNum: 1,
		Board:       make([][]*contracts.Piece, 8),
		DoubleMove:  &contracts.DoubleMoveState{Type: "diff", MovesLeft: 2},
	}
	for i := range state.Board {
		state.Board[i] = make([]*contracts.Piece, 8)
	}
	state.Board[0][7] = &contracts.Piece{Type: "king", Color: "white"}
	state.Board[5][3] = &contracts.Piece{Type: "queen", Color: "white"}
	state.Board[6][1] = &contracts.Piece{Type: "pawn", Color: "white"}
	state.Board[6][5] = &contracts.Piece{Type: "pawn", Color: "white"}
	state.Board[7][3] = &contracts.Piece{Type: "king", Color: "black"}
	state.Board[5][0] = &contracts.Piece{Type: "queen", Color: "black"}
	state.Board[0][0] = &contracts.Piece{Type: "rook", Color: "black"}
	state.Board[0][4] = &contracts.Piece{Type: "pawn", Color: "black", ParasiteTarget: "5,3"}
	return state
}

func TestFirstDoubleMoveRejected(t *testing.T) {
	capture := &Move{From: contracts.Square{Row: 5, Col: 0}, To: contracts.Square{Row: 5, Col: 3}}
	quietRook := &Move{From: contracts.Square{Row: 0, Col: 0}, To: contracts.Square{Row: 0, Col: 1}}

	// The parasite-discovered check: applyMoveCopy alone calls this capture
	// safe (rook (0,0) is still blocked), resolveParasiteEffects does not.
	if !FirstDoubleMoveRejected(firstDoubleMoveBoard(), capture) {
		t.Fatalf("expected the parasite-discovered-check capture to be rejected")
	}
	// A quiet move in the same position must not be swept up by the
	// parasite modeling.
	if FirstDoubleMoveRejected(firstDoubleMoveBoard(), quietRook) {
		t.Fatalf("expected the quiet rook move to stay acceptable")
	}

	// The plain guard still applies without any parasite on the board:
	// rook (0,4)->(0,5) directly checks white king (0,7).
	plain := firstDoubleMoveBoard()
	plain.Board[0][4] = &contracts.Piece{Type: "rook", Color: "black"}
	plain.Board[5][0] = nil
	plain.Board[5][3] = nil
	directCheck := &Move{From: contracts.Square{Row: 0, Col: 4}, To: contracts.Square{Row: 0, Col: 5}}
	if !FirstDoubleMoveRejected(plain, directCheck) {
		t.Fatalf("expected a direct checking first double move to be rejected")
	}

	// Gating: outside the first half the guard (and this predicate) is off.
	noDouble := firstDoubleMoveBoard()
	noDouble.DoubleMove = nil
	if FirstDoubleMoveRejected(noDouble, capture) {
		t.Fatalf("expected false without an active double move")
	}
	secondHalf := firstDoubleMoveBoard()
	secondHalf.DoubleMove = &contracts.DoubleMoveState{Type: "diff", MovesLeft: 1}
	if FirstDoubleMoveRejected(secondHalf, capture) {
		t.Fatalf("expected false on the second half of a double move")
	}
}
