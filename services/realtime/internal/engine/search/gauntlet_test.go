package search

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/engine/core"
)

func TestGauntletSummaryScorePercentAndEloDiff(t *testing.T) {
	cases := []struct {
		name        string
		summary     GauntletSummary
		wantScore   float64
		wantEloSign int // -1, 0, or 1
	}{
		{"all wins", GauntletSummary{Wins: 10}, 1.0, 1},
		{"all losses", GauntletSummary{Losses: 10}, 0.0, -1},
		{"all draws", GauntletSummary{Draws: 10}, 0.5, 0},
		{"even split", GauntletSummary{Wins: 5, Losses: 5}, 0.5, 0},
		{"mostly wins", GauntletSummary{Wins: 7, Draws: 1, Losses: 2}, 0.75, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.summary.ScorePercent()
			if math.Abs(got-c.wantScore) > 1e-9 {
				t.Errorf("ScorePercent: got %v, want %v", got, c.wantScore)
			}
			elo := c.summary.EloDiff()
			switch c.wantEloSign {
			case 1:
				if elo <= 0 {
					t.Errorf("EloDiff: expected positive, got %v", elo)
				}
			case -1:
				if elo >= 0 {
					t.Errorf("EloDiff: expected negative, got %v", elo)
				}
			case 0:
				if math.Abs(elo) > 1e-6 {
					t.Errorf("EloDiff: expected ~0, got %v", elo)
				}
			}
		})
	}
}

func TestGauntletSummaryEloDiffDoesNotOverflowAtExtremes(t *testing.T) {
	allWins := GauntletSummary{Wins: 50}
	elo := allWins.EloDiff()
	if math.IsInf(elo, 0) || math.IsNaN(elo) {
		t.Fatalf("expected a large but finite Elo estimate for a 100%% score, got %v", elo)
	}
}

// TestPlayGauntletGameStrongEvalBeatsInvertedEval is a sanity check for
// the gauntlet mechanics themselves (not a real strength claim about any
// production evaluator): an evaluator that scores every position as the
// NEGATION of the sane placeholder eval actively seeks out bad trades and
// hanging pieces -- it should lose consistently to the real eval across a
// handful of games with colors alternated, confirming PlayGauntletGame
// lets evaluator quality actually decide the outcome (not, say, always
// favoring whichever color moves first or whichever of evalA/evalB is
// passed first).
func TestPlayGauntletGameStrongEvalBeatsInvertedEval(t *testing.T) {
	invertedEval := func(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color) int {
		return -evaluateForMover(p, ov, mover)
	}

	rng := rand.New(rand.NewSource(7))
	var summary GauntletSummary
	const games = 6
	for i := 0; i < games; i++ {
		aColor := core.White
		if i%2 == 1 {
			aColor = core.Black
		}
		result := PlayGauntletGame(DefaultEvaluator, invertedEval, aColor, rng, 20*time.Millisecond, 8, 60, 3)
		switch result {
		case GauntletWin:
			summary.Wins++
		case GauntletDraw:
			summary.Draws++
		case GauntletLoss:
			summary.Losses++
		}
	}
	if summary.Wins == 0 {
		t.Fatalf("expected the sane evaluator (A) to win at least one of %d games against an evaluator that actively seeks bad trades, got %+v", games, summary)
	}
	if summary.Losses > 0 {
		t.Errorf("expected the sane evaluator to never lose to one actively seeking bad trades, got %+v", summary)
	}
}
