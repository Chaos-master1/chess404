package xgauntlet

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

// parasiteDiscoveryDoubleMoveState mirrors internal/match's
// double_move_parasite_test.go board: black's ONLY legal move is the
// capture of white queen (5,3), whose parasite side effect destroys black
// pawn (0,4) -- the only blocker between black rook (0,0) and white king
// (0,7) -- so applyMove rejects the capture with "first double move cannot
// put enemy king in check" even though no plain move simulation sees a
// check.
func parasiteDiscoveryDoubleMoveState() *contracts.MatchState {
	state := &contracts.MatchState{
		MatchID:     "fallback_dm_parasite",
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

// TestFallbackMoveRefusesParasiteDiscoveredFirstDoubleMove pins the
// harness's own progress-guarantee fallback against the failure class that
// caught the engines: whenever MakeMove gives up (pending card, engine
// bail-out) during the first half of a double move, fallbackMove's
// core-model check cannot see parasite side effects, so its first
// candidate was exactly the intent applyMove rejects. Before
// v1.FirstDoubleMoveRejected was added to this filter, fallbackMove
// returned the rejected capture here; now it must refuse (no candidate is
// submittable), which PlayOneGame treats as no-legal-move rather than as a
// rejected intent.
func TestFallbackMoveRefusesParasiteDiscoveredFirstDoubleMove(t *testing.T) {
	intent, err := fallbackMove(parasiteDiscoveryDoubleMoveState())
	if err == nil {
		t.Fatalf("fallbackMove returned %v->%v, but applyMove rejects that first double move (parasite-discovered check)", intent.From, intent.To)
	}
}
