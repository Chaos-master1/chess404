package xgauntlet

import (
	"testing"
	"time"

	v1 "github.com/chess404/realtime/internal/engine/v1"
	"github.com/chess404/realtime/internal/match"
)

// TestPlayOneGameOldVsOldCompletes is the harness's own sanity check: two
// old-engine instances (the one config nobody disputes the strength of)
// playing through a REAL match.Service must produce a decided-or-drawn game
// without error. If this fails, the harness itself is broken, independently
// of anything about the new engine.
func TestPlayOneGameOldVsOldCompletes(t *testing.T) {
	svc := match.NewService()
	defer svc.Close()

	factory := OldEngineFactory(v1.DifficultyBeginner)
	cfg := GameConfig{MaxPly: 60, MaxSubDecisionsPerTurn: 5}

	outcome, err := PlayOneGame(svc, factory, factory, cfg, 1, 4)
	if err != nil {
		t.Fatalf("PlayOneGame errored: %v", err)
	}
	t.Logf("outcome: %v", outcome)
}

// TestPlayOneGameOldVsNewCompletes proves the NewEngineAdapter can actually
// carry a full game through the real intent protocol -- moves AND the
// select_target sequence for whichever of the seven modeled card mechanics
// happen to be drawn -- without the match service ever rejecting an intent.
// This is exactly the failure class that caused the 2026-07-29 "push push
// push" outage for the old engine (an intent the engine could not carry
// through to resolution); this test is the regression guard for the new
// engine making the same class of mistake.
func TestPlayOneGameOldVsNewCompletes(t *testing.T) {
	svc := match.NewService()
	defer svc.Close()

	oldFactory := OldEngineFactory(v1.DifficultyBeginner)
	// depth=1 here, deliberately, not a strength-relevant budget: the turn
	// model means "depth" spans a card-decision node plus a move-decision
	// node PER ply (see NewEngineFactory's doc), so even depth=2 can nest 4
	// applyAndRecurse/negamax levels -- and allowCard's full candidate set
	// (every card's target candidates, unpruned by TT this early) multiplies
	// node count at each level. FairPlaySearchTimed's deadline is only
	// checked BETWEEN whole depths, never within one, so a single expensive
	// depth can run far past its nominal time budget -- this is a genuine
	// search-performance gap (tracked for E2), not a wiring bug, and this
	// smoke test only needs to prove the wiring (intent translation,
	// select_target sequencing) works, not measure strength. depth=1 keeps
	// it fast and reliable.
	newFactory := NewEngineFactory(200*time.Millisecond, 1, 2, 42)
	cfg := GameConfig{MaxPly: 60, MaxSubDecisionsPerTurn: 5}

	outcome, err := PlayOneGame(svc, oldFactory, newFactory, cfg, 2, 4)
	if err != nil {
		t.Fatalf("PlayOneGame(old vs new) errored: %v", err)
	}
	t.Logf("old(white) vs new(black) outcome: %v", outcome)

	outcome2, err := PlayOneGame(svc, newFactory, oldFactory, cfg, 3, 4)
	if err != nil {
		t.Fatalf("PlayOneGame(new vs old) errored: %v", err)
	}
	t.Logf("new(white) vs old(black) outcome: %v", outcome2)
}

// TestRunGauntletProducesAMeasurement is the E0 deliverable's own proof of
// life: a short old-vs-old self-play run (the noise-floor baseline the plan
// calls for) must complete and produce a Summary string without panicking or
// erroring, over several games.
func TestRunGauntletProducesAMeasurement(t *testing.T) {
	svc := match.NewService()
	defer svc.Close()

	a := OldEngineFactory(v1.DifficultyBeginner)
	b := OldEngineFactory(v1.DifficultyBeginner)

	cfg := RunConfig{
		Pairs:        3,
		OpeningPlies: 4,
		Game:         GameConfig{MaxPly: 60, MaxSubDecisionsPerTurn: 5},
		Seed:         7,
		Elo0:         0, Elo1: 10, Alpha: 0.05, Beta: 0.05,
	}
	result := RunGauntlet(svc, a, b, cfg)
	if result.Games() == 0 {
		t.Fatalf("expected at least one game to be played, got 0")
	}
	t.Log(result.Summary("A", "B", cfg.Elo0, cfg.Elo1, cfg.Alpha, cfg.Beta))
}
