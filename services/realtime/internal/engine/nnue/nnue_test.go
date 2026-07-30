package nnue

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

func TestActiveFeaturesExcludesKingsAndRespectsBucket(t *testing.T) {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	feats := ActiveFeatures(p, ov, nil, nil)

	// Starting position: 16 pieces per side minus 2 kings = 30 non-king
	// pieces total, plus 6 always-active count-bucket features (hand
	// size, frozen count, shielded count, each mine/theirs -- "zero" is
	// bucket 0, itself a real active feature, not the absence of one),
	// plus 0 fortress-boolean features (neither side has one active).
	want := 30 + 6
	if len(feats) != want {
		t.Fatalf("expected %d active features at the start position, got %d: %v", want, len(feats), feats)
	}

	bucket := KingBucketOf(p) // e1 -> file 4 (>=4), rank 0 (<4) -> bucket 1
	if bucket != 1 {
		t.Fatalf("expected king bucket 1 for a king on e1, got %d", bucket)
	}
	for _, f := range feats {
		if f < numChessFeatures && f/numPieceSquareFeatures != bucket {
			t.Errorf("feature %d belongs to a different king bucket than the current one (%d)", f, bucket)
		}
	}
}

func TestActiveFeaturesReflectCardState(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	knight := core.NewSquare(1, 0)
	p.SetPiece(knight, core.Piece{Type: core.Knight, Color: core.White})
	ov := core.NewCardOverlay()
	ov.SetFrozen(knight, true)
	ov.SetFortress(core.Black, core.NewSquare(2, 2), 2)

	baseline := ActiveFeatures(p, core.NewCardOverlay(), nil, nil)
	withCards := ActiveFeatures(p, ov, actions.Hand{{ID: "c1", Mechanic: actions.MechanicFreeze}}, nil)

	if len(withCards) <= len(baseline) {
		t.Fatalf("expected more active features once frozen/fortress/hand state is present: baseline=%d withCards=%d", len(baseline), len(withCards))
	}
}

func TestAccumulatorIncrementalMatchesRefresh(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	n := NewRandomNetwork(rng)

	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	features := ActiveFeatures(p, ov, nil, nil)

	var refreshed Accumulator
	n.Refresh(&refreshed, features)

	// Build the same accumulator incrementally: start from an empty one
	// (bias only) and Add every feature one at a time.
	var incremental Accumulator
	n.Refresh(&incremental, nil) // bias only, zero features
	for _, f := range features {
		n.Add(&incremental, f)
	}

	if refreshed != incremental {
		t.Fatal("expected an incrementally-built accumulator to exactly match a full Refresh over the same feature set")
	}

	// Now remove a feature incrementally and confirm it matches a fresh
	// Refresh over the reduced set.
	removed := features[0]
	rest := features[1:]
	n.Remove(&incremental, removed)

	var refreshedRest Accumulator
	n.Refresh(&refreshedRest, rest)
	if incremental != refreshedRest {
		t.Fatal("expected Remove to exactly match a fresh Refresh over the feature set with that one feature excluded")
	}
}

func TestEvaluateDoesNotPanicAndZeroNetworkIsJustBias(t *testing.T) {
	n := &Network{} // all-zero weights and biases
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	var acc Accumulator
	n.Refresh(&acc, ActiveFeatures(p, ov, nil, nil))

	if got := n.Evaluate(&acc); got != 0 {
		t.Fatalf("expected an all-zero network to evaluate to exactly 0, got %d", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	original := NewRandomNetwork(rng)
	original.L1Bias[0] = 12345
	original.OutBias = -777

	var buf bytes.Buffer
	if err := original.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if *loaded != *original {
		t.Fatal("expected the loaded network to exactly match the original")
	}
}

func TestLoadRejectsWrongHeader(t *testing.T) {
	_, err := Load(bytes.NewReader([]byte("NOTANNUE")))
	if err == nil {
		t.Fatal("expected Load to reject a file with the wrong header")
	}
}
