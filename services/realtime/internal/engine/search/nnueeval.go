package search

import (
	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/engine/nnue"
)

// NNUEEvaluator adapts a trained nnue.Network into an Evaluator this
// package's Searcher can use, so Phase 3's network competes in the exact
// same search/gauntlet machinery Phase 2's placeholder eval does.
//
// Computes nnue.ActiveFeatures fresh and Refreshes the accumulator on
// every call, rather than maintaining it incrementally across the search
// tree -- a deliberate Phase 3 scope decision, not an oversight: the
// underlying nnue.Network/Accumulator machinery IS incremental and tested
// as such (nnue_test.go's TestAccumulatorIncrementalMatchesRefresh), but
// wiring incremental maintenance through negamax's make/unmake recursion
// -- including a Refresh whenever White's own king moves (every feature's
// king bucket changes at once) and handling card actions, which change
// overlay-based features without moving any piece at all -- is further
// work, not required to have a real, working, trained network driving
// search decisions today. It also isn't where incremental maintenance
// pays off the most: that's a live game's position changing one move at a
// time across SUCCESSIVE searches (Phase 4's actual target shape), not
// rapid make/unmake within a single search tree.
func NNUEEvaluator(net *nnue.Network) Evaluator {
	return func(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color) int {
		var acc nnue.Accumulator
		net.Refresh(&acc, nnue.ActiveFeatures(p, ov, hands.White, hands.Black))
		score := net.Evaluate(&acc)
		if mover == core.Black {
			return -score
		}
		return score
	}
}
