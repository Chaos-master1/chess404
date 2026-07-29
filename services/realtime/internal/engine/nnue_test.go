package engine

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

func startingBoard() [][]*contracts.Piece {
	return MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1").Board
}

func TestNNUELoaded(t *testing.T) {
	if defaultNNUE == nil {
		t.Fatal("defaultNNUE is nil")
	}
	if !defaultNNUE.Loaded() {
		t.Skip("nnue_weights.bin not found")
	}
	board := startingBoard()
	eval := EvaluateWithModifiers(board, "white", nil, nil, nil, nil, nil)
	t.Logf("NNUE starting position eval: %d", eval)
}

func TestNNUERelativeConsistency(t *testing.T) {
	if !defaultNNUE.Loaded() {
		t.Skip("nnue_weights.bin not found")
	}

	// Starting position eval
	start := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	startEval := EvaluateWithModifiers(start.Board, "white", nil, nil, nil, nil, nil)

	// White up a pawn: remove black pawn at e7
	upPawn := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	upPawn.Board[6][4] = nil // remove white pawn at e2 (black up a pawn from white's perspective? no)
	
	// Actually: white up a pawn = remove a black pawn
	blackDownPawn := MatchStateFromFEN("rnbqkbnr/ppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	bdownEval := EvaluateWithModifiers(blackDownPawn.Board, "white", nil, nil, nil, nil, nil)

	// Both sides equal material
	equal := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq - 0 1")
	evalEqual := EvaluateWithModifiers(equal.Board, "white", nil, nil, nil, nil, nil)

	t.Logf("Start: %d, Black down pawn: %d (diff %d), After e4: %d (diff %d)",
		startEval, bdownEval, bdownEval-startEval, evalEqual, evalEqual-startEval)

	// The key test: NNUE should prefer positions where the side-to-move is better
	// A position after 1.e4 should be roughly similar to starting position
	if evalEqual < bdownEval-300 {
		t.Errorf("NNUE thinks equal position is much worse than white-up-pawn: %d vs %d", evalEqual, bdownEval)
	}
}

func TestNNUESearchPlaysMove(t *testing.T) {
	if !defaultNNUE.Loaded() {
		t.Skip("nnue_weights.bin not found")
	}
	// Quick sanity: can the search find a basic tactic with NNUE?
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	tt := NewTranspositionTable(1 << 16)
	result := SearchWithTime(state, 3, tt, 2000*1000000)
	if result.BestMove.From.Row == 0 && result.BestMove.From.Col == 0 {
		t.Error("Search returned no move")
	}
	t.Logf("Best move: %v -> %v, score=%d, nodes=%d, depth=%d",
		result.BestMove.From, result.BestMove.To, result.Score, result.Nodes, result.Depth)
}
