package search

import (
	"time"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// mateScoreThreshold: any score within this much of scoreMate reflects a
// forced mate found at some small plyFromRoot (see negamax's mate scoring,
// scoreMate-plyFromRoot) -- comfortably above any realistic plyFromRoot
// value, so this never misfires on an ordinary large-but-not-mate eval.
const mateScoreThreshold = scoreMate - 1000

func isMateScore(score int) bool {
	return score >= mateScoreThreshold || score <= -mateScoreThreshold
}

// BestMoveTimed runs iterative deepening (depth 1, 2, 3, ...) against the
// SAME Searcher -- so its TT carries forward and warm-starts each deeper
// iteration -- stopping once elapsed time exceeds timeLimit, checked only
// BETWEEN completed depths, never by aborting a depth already in
// progress: a search that finished depth N is a trustworthy, fully
// alpha-beta-correct result; one interrupted partway through depth N+1 is
// not (its score could reflect an arbitrarily incomplete, misleading
// partial exploration), so this always returns the last FULLY completed
// depth's answer, matching standard engine practice.
//
// This is the fixed-depth BestMove's real-time-budget counterpart --
// BestMove alone has no notion of "think for up to 500ms", which is
// exactly what a difficulty ladder (Task 13) and any genuinely
// time-competitive search need. Depth 1 always runs regardless of how
// small timeLimit is (even 0), guaranteeing SOME legal result rather than
// none. Deepening stops early once a forced mate is found (searching
// deeper than a confirmed mate wastes the remaining budget on a question
// already answered) or maxDepth is reached.
//
// Returns the same (Action, score, ok) as BestMove, plus the deepest
// depth actually completed. ok is false only in BestMove's own "no action
// available" case (see its doc comment) -- callers must not apply the
// returned Move when ok is false.
func (s *Searcher) BestMoveTimed(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, allowCard bool, timeLimit time.Duration, maxDepth int) (best actions.Action, score int, depthReached int, ok bool) {
	deadline := time.Now().Add(timeLimit)

	for depth := 1; depth <= maxDepth; depth++ {
		if depth > 1 && time.Now().After(deadline) {
			break
		}
		a, sc, found := s.BestMove(p, ov, hands, mover, allowCard, depth)
		if !found {
			return actions.Action{}, 0, 0, false
		}
		best, score, depthReached, ok = a, sc, depth, true
		if isMateScore(sc) || time.Now().After(deadline) {
			break
		}
	}
	return best, score, depthReached, ok
}
