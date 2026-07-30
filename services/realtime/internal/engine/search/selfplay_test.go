package search

import (
	"math/rand"
	"testing"

	"github.com/chess404/realtime/internal/engine/core"
)

func TestGenerateSelfPlayGameProducesValidRecords(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	records := GenerateSelfPlayGame(defaultEvaluator, rng, 2, 20, 4)

	if len(records) == 0 {
		t.Fatal("expected at least one recorded position")
	}
	for i, r := range records {
		if _, err := core.ParseFEN(r.FEN); err != nil {
			t.Fatalf("record %d: FEN %q does not parse: %v", i, r.FEN, err)
		}
		// Label is +/-outcomeScale for a genuine checkmate, 0 for a genuine
		// stalemate, or (GenerateSelfPlayGame's `decided` handling) any
		// value in between for a game adjudicated by final material
		// balance -- so the only universal invariant is the same [-scale,
		// scale] range a checkmate itself produces.
		if r.Label < -outcomeScale || r.Label > outcomeScale {
			t.Fatalf("record %d: expected Label within [-%v, %v], got %v", i, outcomeScale, outcomeScale, r.Label)
		}
		if r.WhiteHandSize < 0 || r.BlackHandSize < 0 {
			t.Fatalf("record %d: negative hand size (white=%d black=%d)", i, r.WhiteHandSize, r.BlackHandSize)
		}
	}
}

// TestSelfPlayActuallyPlaysCards is Task 9's actual requirement, checked
// directly rather than assumed: across several games, at least one must
// show a hand size decreasing between consecutive records for the same
// side -- proof a card was genuinely played, not just dealt and ignored
// (the old pipeline's flaw the plan calls out: selfplay.go there only
// ever calls applyMoveCopy, so hands are dealt but never spent).
func TestSelfPlayActuallyPlaysCards(t *testing.T) {
	cardWasPlayed := false
	for seed := int64(0); seed < 15 && !cardWasPlayed; seed++ {
		rng := rand.New(rand.NewSource(seed))
		records := GenerateSelfPlayGame(defaultEvaluator, rng, 2, 30, 5)
		for i := 1; i < len(records); i++ {
			if records[i].WhiteHandSize < records[i-1].WhiteHandSize || records[i].BlackHandSize < records[i-1].BlackHandSize {
				cardWasPlayed = true
				break
			}
		}
	}
	if !cardWasPlayed {
		t.Fatal("expected at least one card to be played across 15 self-play games, but no hand size ever decreased")
	}
}
