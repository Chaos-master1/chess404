package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

func startingBoard() [][]*contracts.Piece {
	return MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1").Board
}

// TestNNUELoadPrefersExplicitPathEnvVar is a regression test for why
// production never found the weights file at all: match-service's runtime
// image never shipped it, and even if it had, the container's working
// directory (/) doesn't satisfy any of Load's relative fallback paths. The
// fix ships the file to a fixed location and points CHESS404_NNUE_WEIGHTS_PATH
// at it (see match-service.Dockerfile) -- this pins that the env var actually
// takes priority over the relative guesses, using a file the relative
// fallbacks could not possibly find (an isolated temp directory).
func TestNNUELoadPrefersExplicitPathEnvVar(t *testing.T) {
	// Load rejects any header shape other than the package's fixed
	// architecture (nnueInputSize/nnueHiddenSize/nnueHidden2Size), so the test
	// fixture has to declare those exact dimensions -- an arbitrary small
	// shape fails validation before the env var precedence being tested here
	// is even reached.
	weights := buildMinimalNNUEWeights(t, nnueInputSize, nnueHiddenSize, nnueHidden2Size)

	dir := t.TempDir()
	path := filepath.Join(dir, "weights.bin")
	if err := os.WriteFile(path, weights, 0o644); err != nil {
		t.Fatalf("failed to write test weights file: %v", err)
	}
	t.Setenv("CHESS404_NNUE_WEIGHTS_PATH", path)

	n := &NNUE{}
	if err := n.Load("this-relative-path-does-not-exist.bin"); err != nil {
		t.Fatalf("expected Load to find the file via CHESS404_NNUE_WEIGHTS_PATH, got %v", err)
	}
	if !n.Loaded() {
		t.Fatal("expected the network to report loaded after a successful Load")
	}
}

// buildMinimalNNUEWeights constructs the smallest byte-valid weights file for
// the given (in, h1, h2) shape: a 12-byte header followed by
// W0(in*h1) B0(h1) W1(h1*h2) B1(h2) W2(h2*1) B2(1) little-endian float32s, all
// zero. Mirrors the layout nnue.go's Load parses.
func buildMinimalNNUEWeights(t *testing.T, in, h1, h2 int) []byte {
	t.Helper()
	floatCount := in*h1 + h1 + h1*h2 + h2 + h2*1 + 1
	buf := make([]byte, 12+floatCount*4)
	putU32 := func(off int, v int) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	putU32(0, in)
	putU32(4, h1)
	putU32(8, h2)
	return buf
}

func TestNNUELoaded(t *testing.T) {
	// EvaluateWithModifiers defaults to the hand-crafted evaluation now (see
	// nnueEnabled in nnue.go) -- without this, every test below exercised
	// ClassicalEval and not NNUE at all, despite what their names claim, and
	// their loose assertions ("roughly consistent", "a move was returned")
	// would have kept passing even if NNUE.Evaluate itself were broken.
	t.Setenv("CHESS404_ENGINE_NNUE", "1")

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
	t.Setenv("CHESS404_ENGINE_NNUE", "1")
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

// TestNNUEBoardEncodingContractWithTrainer pins the exact input-feature index
// encodeBoard must produce for two reference squares, computed independently
// by hand from the SAME formula scripts/train_nnue.py's (now-fixed) encode_fen
// uses -- (colorIdx*6+typeIdx)*64 + boardRow*8+col, with boardRow counted from
// rank 1 -- so a regression on either side (Go re-introducing some other
// convention, or the trainer's `7 - r` conversion being reverted) shows up
// here instead of only as an unexplained drop in playing strength months
// later after a very expensive retrain.
//
// Before the trainer's fix, it computed sq using the raw top-to-bottom FEN
// row instead of `7 - r`, i.e. it treated a8 as square 0. That is caught here
// too: recomputing these expected indices with the raw FEN row instead of the
// corrected boardRow gives 248 and 455, not 192 and 511 -- the two encoders
// disagreed on every square except the four where a board is its own vertical
// mirror.
func TestNNUEBoardEncodingContractWithTrainer(t *testing.T) {
	n := &NNUE{}

	cases := []struct {
		name string
		fen  string
		want int
	}{
		// White rook on a1: colorIdx=0, typeIdx=3 (rook), boardRow=0, col=0.
		// (0*6+3)*64 + (0*8+0) = 192.
		{"white rook a1", "8/8/8/8/8/8/8/R7 w - - 0 1", 192},
		// Black knight on h8: colorIdx=1, typeIdx=1 (knight), boardRow=7, col=7.
		// (1*6+1)*64 + (7*8+7) = 448 + 63 = 511.
		{"black knight h8", "7n/8/8/8/8/8/8/8 w - - 0 1", 511},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			board := MatchStateFromFEN(tc.fen).Board
			input := make([]float32, nnueInputSize)
			n.encodeBoard(board, input)

			if input[tc.want] != 1.0 {
				t.Fatalf("expected feature index %d to be set for %s, it was %v", tc.want, tc.name, input[tc.want])
			}
			set := 0
			for _, v := range input {
				if v != 0 {
					set++
				}
			}
			if set != 1 {
				t.Fatalf("expected exactly one board feature set for a single piece, got %d", set)
			}
		})
	}
}

func TestNNUESearchPlaysMove(t *testing.T) {
	t.Setenv("CHESS404_ENGINE_NNUE", "1")
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
