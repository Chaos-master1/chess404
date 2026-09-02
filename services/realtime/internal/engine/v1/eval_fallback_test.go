package v1

import "testing"

// These tests pin down the two properties EvaluateWithModifiers must have and
// did not: it must never return a constant 0 just because the learned weights
// are unavailable, and it must return an ABSOLUTE (White-positive) score.
//
// Before this fix, EvaluateWithModifiers short-circuited to `return 0` whenever
// the network was not loaded -- and the weights file is never copied into the
// runtime image, so in production every position scored exactly 0 and the
// search was left running on move ordering and mate scores alone. The
// hand-crafted evaluation existed, fully implemented, and was reachable only
// from tests. Note these tests would have passed in CI even then, because CI
// runs from services/realtime where the loader's "../../" probe happens to
// resolve -- tests and production were exercising different evaluators.

// TestEvalNeverReturnsZeroWithoutNNUE is the direct regression guard: with the
// learned evaluation off (the default), a clearly unequal position must still
// produce a non-zero score.
func TestEvalNeverReturnsZeroWithoutNNUE(t *testing.T) {
	t.Setenv("CHESS404_ENGINE_NNUE", "0")

	// White is a full queen up.
	board := MatchStateFromFEN("rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1").Board

	got := EvaluateWithModifiers(board, "white", nil, nil, nil, nil, nil)
	if got == 0 {
		t.Fatal("evaluation returned 0 for a position where White is a queen up -- the search cannot distinguish any position from any other")
	}
	if got < 500 {
		t.Fatalf("expected White to be evaluated clearly ahead when a queen up, got %d", got)
	}
}

// TestEvalIsAbsoluteNotSideRelative locks the sign contract. search.go negates
// this function's result itself (`if !maximizing { v = -v }`, where maximizing
// is `state.Turn == "white"` at every ply), so returning a side-to-move-relative
// score here double-negates for Black. The old NNUE branch did exactly that.
func TestEvalIsAbsoluteNotSideRelative(t *testing.T) {
	t.Setenv("CHESS404_ENGINE_NNUE", "0")

	// Identical position, evaluated once with each side to move. White is up a
	// queen in both cases, so both must report a positive (White-favouring)
	// score. If the score flips sign with the turn, it is side-relative.
	board := MatchStateFromFEN("rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1").Board

	white := EvaluateWithModifiers(board, "white", nil, nil, nil, nil, nil)
	black := EvaluateWithModifiers(board, "black", nil, nil, nil, nil, nil)

	if white <= 0 {
		t.Fatalf("expected a positive (White-favouring) score with White to move, got %d", white)
	}
	if black <= 0 {
		t.Fatalf("expected the SAME White-favouring sign with Black to move -- the score must be absolute, not side-relative -- got %d", black)
	}
}

// TestEvalSymmetricStartIsNearZero is a sanity check the current learned
// weights fail badly (they score this at about -322cp), which is why the
// hand-crafted evaluation is the default.
func TestEvalSymmetricStartIsNearZero(t *testing.T) {
	t.Setenv("CHESS404_ENGINE_NNUE", "0")

	got := EvaluateWithModifiers(startingBoard(), "white", nil, nil, nil, nil, nil)
	if got < -100 || got > 100 {
		t.Fatalf("expected the symmetric starting position to evaluate near equal, got %d", got)
	}
}
