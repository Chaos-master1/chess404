package search

import (
	"math"
	"math/rand"
	"time"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// GauntletResult is one game's outcome from engineA's perspective (the
// evaluator passed as evalA to PlayGauntletGame), regardless of which
// color it actually played that game -- callers alternate colors across
// a match to cancel out first-move advantage, so results stay directly
// comparable game to game only once already normalized to "how did A do"
// rather than "how did White do".
type GauntletResult int

const (
	GauntletWin GauntletResult = iota
	GauntletDraw
	GauntletLoss
)

// decisiveAdjudicationMargin is the materialScore magnitude (eval.go's
// centipawn-ish units) an unfinished gauntlet game must reach to count as
// a decisive win/loss rather than a draw -- roughly three pawns, a solid
// and fairly unambiguous edge, not a marginal one. Standard practice for
// engine-vs-engine testing at fast time controls, where games routinely
// don't run to an actual mate: adjudicate by a clear material margin
// instead of counting every unfinished game as a draw, which would wash
// out real strength differences between the two evaluators under test.
const decisiveAdjudicationMargin = 300

// PlayGauntletGame plays one game between evalA (playing aColor) and
// evalB (playing the opposite color), each side searching with its OWN
// evaluator via BestMoveTimed, and returns the result from evalA's
// perspective. Mirrors GenerateSelfPlayGame's turn loop (real sampled
// hands, cards genuinely played, the same graceful "no action available"
// handling) but with two independently-evaluated sides instead of one
// shared evaluator, and a discrete W/D/L result instead of a continuous
// training label.
func PlayGauntletGame(evalA, evalB Evaluator, aColor core.Color, rng *rand.Rand, timePerMove time.Duration, maxDepth, maxPlies, handSize int) GauntletResult {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	hands := Hands{
		White: actions.SampleHand(rng, handSize),
		Black: actions.SampleHand(rng, handSize),
	}

	evalFor := func(c core.Color) Evaluator {
		if c == aColor {
			return evalA
		}
		return evalB
	}

	mover := core.White
	for ply := 0; ply < maxPlies; ply++ {
		status := actions.TerminalStatus(p, ov, hands.For(mover))
		if status == core.Checkmate {
			winner := mover.Opposite()
			return resultFor(winner, aColor)
		}
		if status == core.Stalemate {
			return GauntletDraw
		}

		s := NewSearcherWithEvalAndTTSize(evalFor(mover), selfPlayTTSlots)
		chosen, _, _, ok := s.BestMoveTimed(p, ov, hands, mover, true, timePerMove, maxDepth)
		if !ok {
			return adjudicateGauntletDraw(p, aColor)
		}
		if chosen.Kind == actions.ActionCard {
			u := actions.ApplyCardAction(p, ov, chosen)
			_ = u
			hands = hands.With(mover, hands.For(mover).Without(chosen.Card.ID))

			s2 := NewSearcherWithEvalAndTTSize(evalFor(mover), selfPlayTTSlots)
			chosen, _, _, ok = s2.BestMoveTimed(p, ov, hands, mover, false, timePerMove, maxDepth)
			if !ok {
				return adjudicateGauntletDraw(p, aColor)
			}
		}

		_ = applyMoveWithTicks(p, ov, mover, chosen.Move)
		mover = mover.Opposite()
	}

	return adjudicateGauntletDraw(p, aColor)
}

func resultFor(winner, aColor core.Color) GauntletResult {
	if winner == aColor {
		return GauntletWin
	}
	return GauntletLoss
}

// adjudicateGauntletDraw grades an unfinished game by final material
// balance, from A's own perspective (materialScore is White-perspective;
// negate it when A played Black so "positive" always means "good for A").
func adjudicateGauntletDraw(p *core.Position, aColor core.Color) GauntletResult {
	balance := materialScore(p)
	if aColor == core.Black {
		balance = -balance
	}
	switch {
	case balance >= decisiveAdjudicationMargin:
		return GauntletWin
	case balance <= -decisiveAdjudicationMargin:
		return GauntletLoss
	default:
		return GauntletDraw
	}
}

// GauntletSummary tallies a match's results and derives a win-rate-based
// Elo difference estimate.
type GauntletSummary struct {
	Wins, Draws, Losses int
}

func (g GauntletSummary) Games() int { return g.Wins + g.Draws + g.Losses }

// ScorePercent is the standard tournament scoring convention (a win = 1
// point, a draw = 0.5, a loss = 0), as a fraction of games played.
func (g GauntletSummary) ScorePercent() float64 {
	games := g.Games()
	if games == 0 {
		return 0.5
	}
	return (float64(g.Wins) + 0.5*float64(g.Draws)) / float64(games)
}

// EloDiff estimates the Elo rating difference implied by ScorePercent,
// using the standard logistic relationship real rating systems (FIDE,
// chess engine testing frameworks) use to convert a score percentage into
// a rating gap: diff = 400 * log10(score / (1 - score)). Undefined at the
// boundaries (100% or 0% score); clamped just inside them so a small
// sample's extreme result still returns a large-but-finite number instead
// of +/-Inf.
func (g GauntletSummary) EloDiff() float64 {
	score := g.ScorePercent()
	const epsilon = 1e-4
	if score >= 1-epsilon {
		score = 1 - epsilon
	}
	if score <= epsilon {
		score = epsilon
	}
	return 400 * math.Log10(score/(1-score))
}
