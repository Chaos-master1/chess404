// Command xgauntlet runs the E0 cross-engine gauntlet and prints a
// gauntlet.GauntletResult-style summary. Two modes:
//
//	xgauntlet -mode=baseline           old engine vs itself (noise floor)
//	xgauntlet -mode=cross -difficulty=medium   old engine vs the new search
//
// See internal/engine/xgauntlet's package doc for why this exists and what
// it drives games through.
package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/chess404/realtime/internal/engine"
	"github.com/chess404/realtime/internal/engine/xgauntlet"
	"github.com/chess404/realtime/internal/match"
)

func main() {
	mode := flag.String("mode", "baseline", "baseline (old-vs-old noise floor) or cross (old-vs-new)")
	difficulty := flag.String("difficulty", "medium", "old engine difficulty: beginner|easy|medium|hard|expert")
	pairs := flag.Int("pairs", 30, "opening pairs (each played twice, colour-swapped)")
	openingPlies := flag.Int("openingPlies", 6, "random opening plies before engines take over")
	newTimeLimit := flag.Duration("newTimeLimit", 300*time.Millisecond, "new engine per-decision time budget")
	newDepth := flag.Int("newDepth", 2, "new engine max search depth")
	newSamples := flag.Int("newSamples", 4, "new engine PIMC sample count")
	seed := flag.Int64("seed", 1, "base seed")
	flag.Parse()

	svc := match.NewService()
	defer svc.Close()

	diff := engine.ParseDifficulty(*difficulty)
	oldFactory := xgauntlet.OldEngineFactory(diff)

	var a, b xgauntlet.EngineFactory
	var aName, bName string
	switch *mode {
	case "baseline":
		a, b = oldFactory, oldFactory
		aName, bName = "old-"+*difficulty, "old-"+*difficulty
	case "cross":
		a, b = oldFactory, xgauntlet.NewEngineFactory(*newTimeLimit, *newDepth, *newSamples, *seed+1000)
		aName, bName = "old-"+*difficulty, "new-engine"
	default:
		fmt.Printf("unknown -mode %q (want baseline|cross)\n", *mode)
		return
	}

	cfg := xgauntlet.RunConfig{
		Pairs:        *pairs,
		OpeningPlies: *openingPlies,
		Game:         xgauntlet.DefaultGameConfig(),
		Seed:         *seed,
		Elo0:         0, Elo1: 10, Alpha: 0.05, Beta: 0.05,
		OnGame: func(played int, r engine.GauntletResult) {
			fmt.Printf("\r%d games  W%d L%d D%d  score %.3f", played, r.AWins, r.BWins, r.Draws, r.Score())
		},
	}

	result := xgauntlet.RunGauntlet(svc, a, b, cfg)
	fmt.Println()
	fmt.Println(result.Summary(aName, bName, cfg.Elo0, cfg.Elo1, cfg.Alpha, cfg.Beta))
}
