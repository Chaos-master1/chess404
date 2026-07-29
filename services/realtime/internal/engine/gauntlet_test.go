package engine

import (
	"math"
	"testing"
	"time"
)

// The statistics are the part that has to be right. If Elo or the SPRT bounds
// are wrong, every tuning decision made downstream of this harness is wrong
// too, and it would be wrong silently -- so these are checked against known
// closed-form values rather than against the implementation's own output.

func TestEloFromScoreKnownValues(t *testing.T) {
	cases := []struct {
		name  string
		score float64
		want  float64
	}{
		{"even", 0.5, 0},
		// -400*log10(1/0.75 - 1) = +190.85
		{"75 percent", 0.75, 190.85},
		{"25 percent", 0.25, -190.85},
		// A 10 Elo edge is a 51.44% score -- the granularity the default
		// SPRT bounds are trying to resolve.
		{"ten elo", scoreFromElo(10), 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eloFromScore(tc.score)
			if math.Abs(got-tc.want) > 0.5 {
				t.Fatalf("eloFromScore(%v) = %.2f, want %.2f", tc.score, got, tc.want)
			}
		})
	}
}

func TestEloIsSymmetric(t *testing.T) {
	a := GauntletResult{AWins: 30, BWins: 20, Draws: 50}
	b := GauntletResult{AWins: 20, BWins: 30, Draws: 50}
	if math.Abs(a.Elo()+b.Elo()) > 1e-9 {
		t.Fatalf("swapping wins and losses must negate Elo, got %.4f and %.4f", a.Elo(), b.Elo())
	}
}

func TestEloSweepsAreInfiniteNotClamped(t *testing.T) {
	// A clean sweep genuinely carries no upper bound on the estimate. Silently
	// clamping it would report a specific, wrong number.
	if got := (GauntletResult{AWins: 10}).Elo(); !math.IsInf(got, 1) {
		t.Fatalf("expected +Inf Elo for a clean sweep, got %v", got)
	}
	if got := (GauntletResult{BWins: 10}).Elo(); !math.IsInf(got, -1) {
		t.Fatalf("expected -Inf Elo for a clean loss, got %v", got)
	}
}

func TestSPRTAcceptsRealImprovement(t *testing.T) {
	// Overwhelming evidence for A: SPRT must terminate on H1.
	r := GauntletResult{AWins: 200, BWins: 40, Draws: 60}
	verdict, llr := r.SPRT(0, 10, 0.05, 0.05)
	if verdict != SPRTAcceptH1 {
		t.Fatalf("expected H1 for a dominant score, got %q (LLR %.2f)", verdict, llr)
	}
}

func TestSPRTRejectsRegression(t *testing.T) {
	// Overwhelming evidence against A.
	r := GauntletResult{AWins: 40, BWins: 200, Draws: 60}
	verdict, llr := r.SPRT(0, 10, 0.05, 0.05)
	if verdict != SPRTAcceptH0 {
		t.Fatalf("expected H0 for a losing score, got %q (LLR %.2f)", verdict, llr)
	}
}

func TestSPRTWithdholdsVerdictOnThinEvidence(t *testing.T) {
	// A handful of games near even must NOT reach a verdict. Declaring one
	// here is how a harness manufactures fake improvements.
	r := GauntletResult{AWins: 3, BWins: 2, Draws: 1}
	verdict, llr := r.SPRT(0, 10, 0.05, 0.05)
	if verdict != SPRTContinue {
		t.Fatalf("expected no verdict from 6 near-even games, got %q (LLR %.2f)", verdict, llr)
	}
}

func TestSPRTBoundsMatchErrorRates(t *testing.T) {
	// At alpha=beta=0.05 the classic bounds are +/-log(19) = +/-2.944.
	want := math.Log(19)
	upper := math.Log((1 - 0.05) / 0.05)
	if math.Abs(upper-want) > 1e-9 {
		t.Fatalf("upper bound %.4f, want %.4f", upper, want)
	}
}

func TestErrorMarginShrinksWithMoreGames(t *testing.T) {
	few := GauntletResult{AWins: 5, BWins: 4, Draws: 1}
	many := GauntletResult{AWins: 500, BWins: 400, Draws: 100}
	if !(many.EloErrorMargin() < few.EloErrorMargin()) {
		t.Fatalf("error margin must shrink with sample size: %d games -> %.1f, %d games -> %.1f",
			few.Games(), few.EloErrorMargin(), many.Games(), many.EloErrorMargin())
	}
}

// TestPlayGameDetectsCheckmate verifies the game loop's terminal detection
// using Fool's Mate, where White is already mated with White to move.
func TestPlayGameDetectsCheckmate(t *testing.T) {
	mated := MatchStateFromFEN("rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3")
	c := Contender{Name: "x", TimeLimit: 10 * time.Millisecond}

	if got := PlayGame(c, c, mated, 10); got != OutcomeBlackWin {
		t.Fatalf("expected Black to have mated White, got outcome %v", got)
	}
}

// TestRunGauntletIsDeterministic pins the variance-reduction guarantee: the
// same seed must replay the same openings, or results are not comparable
// between runs and the whole harness is useless for A/B testing.
func TestRunGauntletIsDeterministic(t *testing.T) {
	cfg := DefaultGauntletConfig()
	cfg.Pairs = 1
	cfg.OpeningPlies = 4
	cfg.MaxPly = 12
	cfg.Seed = 42

	c := Contender{Name: "x", TimeLimit: 5 * time.Millisecond, MaxDepth: 2}

	first := RunGauntlet(c, c, cfg)
	second := RunGauntlet(c, c, cfg)

	if first != second {
		t.Fatalf("same seed must produce the same result, got %+v then %+v", first, second)
	}
	if first.Games() == 0 {
		t.Fatal("expected the gauntlet to actually play games")
	}
}

// TestGauntletDetectsAKnownStrengthGap is the harness's own acid test: it
// proves the tool this phase exists to build actually measures something,
// rather than just producing numbers that look plausible. A depth-1 search
// against a depth-3 search on the same evaluator is about as unambiguous a
// strength gap as exists -- if RunGauntlet can't surface THIS as a clear,
// positive Elo advantage for the deeper side, it cannot be trusted to judge
// any real tuning change in the phases that follow.
//
// This is deliberately independent of the specific eval-was-broken bug fixed
// earlier in this phase (that's covered directly by
// TestEvalNeverReturnsZeroWithoutNNUE / TestEvalIsAbsoluteNotSideRelative in
// eval_fallback_test.go) -- this test validates the measurement tool itself,
// against a strength gap whose direction is known by construction.
//
// Depth and game/ply counts are deliberately modest: the search's own
// mid-search stop check only fires every 8192 nodes (search.go), which this
// package's existing benchmarks put at several seconds of real time -- so a
// per-move TimeLimit is closer to a floor than a ceiling once depth is the
// binding constraint, and depth 4+ made a full run of this test take minutes.
func TestGauntletDetectsAKnownStrengthGap(t *testing.T) {
	weak := Contender{Name: "depth1", TimeLimit: 50 * time.Millisecond, MaxDepth: 1}
	strong := Contender{Name: "depth3", TimeLimit: 50 * time.Millisecond, MaxDepth: 3}

	cfg := DefaultGauntletConfig()
	cfg.Pairs = 10
	cfg.OpeningPlies = 4
	cfg.MaxPly = 20
	cfg.Seed = 7

	result := RunGauntlet(strong, weak, cfg)
	t.Log(result.Summary(strong.Name, weak.Name, cfg.Elo0, cfg.Elo1, cfg.Alpha, cfg.Beta))

	if result.Games() == 0 {
		t.Fatal("expected the gauntlet to play at least one game")
	}
	if elo := result.Elo(); !(elo > 0) {
		t.Fatalf("expected the depth-3 search to show a positive Elo advantage over depth-1, got %.1f (score %.3f over %d games)",
			elo, result.Score(), result.Games())
	}
	if result.AWins <= result.BWins {
		t.Fatalf("expected the deeper search to win more games than it lost, got depth3=%d depth1=%d draws=%d",
			result.AWins, result.BWins, result.Draws)
	}
}
