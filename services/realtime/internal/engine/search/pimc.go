package search

import (
	"math/rand"
	"sort"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// SearchResult is one root action's PIMC-aggregated score.
type SearchResult struct {
	Action actions.Action
	Score  float64
}

// FairPlaySearch is the fair-play entry point: myHand is the searching
// engine's REAL hand, but the opponent's hand is never read -- only
// opponentHandSize (always public: how many cards someone holds is
// visible even where card CONTENTS are hidden). For each of `samples`
// PIMC iterations, it draws a fresh plausible opponent hand
// (actions.SampleHand) and solves the resulting fully-known position to
// `depth` for every root action (not just the single best), then averages
// each action's score across all samples -- standard Perfect-Information
// Monte Carlo: solve each hidden-information scenario with full
// information, then aggregate. Returned in descending score order, so
// callers can take Results[0].Action as the move to actually play.
//
// No chance nodes are needed anywhere in this package: internal/match's
// draws are deterministic given RNGSeed+FullMoveNum (cards.go's
// deterministicCardIndex), not a real random event mid-game -- the only
// uncertainty is which hand the opponent actually holds, which is exactly
// what sampling here addresses. rng is the SAMPLING source (which
// plausible hand to imagine next), unrelated to and never seeded from the
// match's own RNGSeed.
func FairPlaySearch(p *core.Position, ov *core.CardOverlay, myHand actions.Hand, myColor core.Color, opponentHandSize, samples, depth int, rng *rand.Rand) []SearchResult {
	root := actions.GenerateActions(p, ov, myHand, true)
	if len(root) == 0 {
		return nil
	}

	totals := make([]float64, len(root))
	for iter := 0; iter < samples; iter++ {
		opponentHand := actions.SampleHand(rng, opponentHandSize)
		hands := Hands{}.With(myColor, myHand).With(myColor.Opposite(), opponentHand)
		s := NewSearcher()
		for i, a := range root {
			score := s.applyAndRecurse(p, ov, hands, myColor, a, depth, 1, -scoreInfinity, scoreInfinity)
			totals[i] += float64(score)
		}
	}

	results := make([]SearchResult, len(root))
	for i, a := range root {
		results[i] = SearchResult{Action: a, Score: totals[i] / float64(samples)}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results
}
