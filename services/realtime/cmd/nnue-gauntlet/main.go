// Command nnue-gauntlet plays a head-to-head match between Phase 3's
// trained NNUE and Phase 2's placeholder material+overlay eval -- Task
// 10's gauntlet, empirically answering "does the trained network actually
// play better" rather than assuming it from training loss alone.
//
// Example:
//
//	go run ./cmd/nnue-gauntlet -weights internal/engine/nnue/pytrainer/trained.bin -games 200
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/engine/nnue"
	"github.com/chess404/realtime/internal/engine/search"
)

func main() {
	weightsPath := flag.String("weights", "", "path to a trained nnue.Network weights file (required)")
	games := flag.Int("games", 200, "number of games to play (split evenly, alternating which side the NNUE plays)")
	msPerMove := flag.Int("ms-per-move", 150, "time budget per move, in milliseconds")
	maxDepth := flag.Int("max-depth", 32, "iterative-deepening depth cap")
	maxPlies := flag.Int("plies", 120, "maximum plies per game before adjudication")
	handSize := flag.Int("hand", 3, "cards dealt to each side at game start")
	seed := flag.Int64("seed", 1, "PRNG seed")
	flag.Parse()

	if *weightsPath == "" {
		fmt.Fprintln(os.Stderr, "nnue-gauntlet: -weights is required")
		os.Exit(1)
	}

	f, err := os.Open(*weightsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nnue-gauntlet: opening weights file:", err)
		os.Exit(1)
	}
	net, err := nnue.Load(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "nnue-gauntlet: loading network:", err)
		os.Exit(1)
	}

	nnueEval := search.NNUEEvaluator(net)
	placeholderEval := search.DefaultEvaluator
	rng := rand.New(rand.NewSource(*seed))
	timePerMove := time.Duration(*msPerMove) * time.Millisecond

	var summary search.GauntletSummary
	for i := 0; i < *games; i++ {
		// NNUE plays White on even iterations, Black on odd -- alternating
		// colors across the match cancels out first-move advantage rather
		// than letting it bias the result toward whichever side happened
		// to move first more often.
		nnueColor := "white"
		var result search.GauntletResult
		if i%2 == 0 {
			result = search.PlayGauntletGame(nnueEval, placeholderEval, core.White, rng, timePerMove, *maxDepth, *maxPlies, *handSize)
		} else {
			nnueColor = "black"
			result = search.PlayGauntletGame(nnueEval, placeholderEval, core.Black, rng, timePerMove, *maxDepth, *maxPlies, *handSize)
		}

		switch result {
		case search.GauntletWin:
			summary.Wins++
		case search.GauntletDraw:
			summary.Draws++
		case search.GauntletLoss:
			summary.Losses++
		}

		if (i+1)%10 == 0 || i+1 == *games {
			fmt.Fprintf(os.Stderr, "nnue-gauntlet: %d/%d games (last: NNUE as %s -> %v) -- W%d D%d L%d\n",
				i+1, *games, nnueColor, result, summary.Wins, summary.Draws, summary.Losses)
		}
	}

	result := struct {
		Games     int     `json:"games"`
		Wins      int     `json:"wins"`
		Draws     int     `json:"draws"`
		Losses    int     `json:"losses"`
		ScorePct  float64 `json:"scorePercent"`
		EloDiff   float64 `json:"eloDiff"`
		MsPerMove int     `json:"msPerMove"`
		MaxDepth  int     `json:"maxDepth"`
		MaxPlies  int     `json:"maxPlies"`
	}{
		Games:     summary.Games(),
		Wins:      summary.Wins,
		Draws:     summary.Draws,
		Losses:    summary.Losses,
		ScorePct:  summary.ScorePercent() * 100,
		EloDiff:   summary.EloDiff(),
		MsPerMove: *msPerMove,
		MaxDepth:  *maxDepth,
		MaxPlies:  *maxPlies,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}
