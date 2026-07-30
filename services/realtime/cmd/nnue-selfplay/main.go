// Command nnue-selfplay generates self-play games -- genuinely playing
// cards, not just dealing them, see search.GenerateSelfPlayGame -- and
// writes one JSON line per recorded position to stdout or a file, in
// exactly the field set nnue/pytrainer/train.py consumes to build a
// training set for Phase 3's NNUE (Task 8).
//
// Example (timed search, the higher-quality default -- see
// GenerateSelfPlayGameTimed's doc comment for why fixed low depth
// produces weak training data):
//
//	go run ./cmd/nnue-selfplay -games 200 -out selfplay.jsonl
//	python internal/engine/nnue/pytrainer/train.py selfplay.jsonl trained.bin
//
// Pass -depth to use a fixed search depth instead (mainly for comparing
// against earlier fixed-depth-2 datasets).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/chess404/realtime/internal/engine/search"
)

func main() {
	games := flag.Int("games", 200, "number of self-play games to generate")
	depth := flag.Int("depth", 0, "fixed search depth per move (0 = use -ms-per-move timed search instead)")
	msPerMove := flag.Int("ms-per-move", 200, "time budget per move in milliseconds, when -depth is 0")
	maxDepth := flag.Int("max-depth", 32, "iterative-deepening depth cap, when -depth is 0")
	maxPlies := flag.Int("plies", 120, "maximum plies per game")
	handSize := flag.Int("hand", 3, "cards dealt to each side at game start")
	seed := flag.Int64("seed", 1, "PRNG seed")
	outPath := flag.String("out", "", "output JSONL path (default: stdout)")
	flag.Parse()

	w := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "nnue-selfplay: creating output file:", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	rng := rand.New(rand.NewSource(*seed))
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	enc := json.NewEncoder(bw)

	timed := *depth <= 0
	if timed {
		fmt.Fprintf(os.Stderr, "nnue-selfplay: using timed search (%dms/move, max depth %d)\n", *msPerMove, *maxDepth)
	} else {
		fmt.Fprintf(os.Stderr, "nnue-selfplay: using fixed depth %d\n", *depth)
	}

	totalRecords := 0
	for g := 0; g < *games; g++ {
		var records []search.SelfPlayRecord
		if timed {
			records = search.GenerateSelfPlayGameTimed(search.DefaultEvaluator, rng, time.Duration(*msPerMove)*time.Millisecond, *maxDepth, *maxPlies, *handSize)
		} else {
			records = search.GenerateSelfPlayGame(search.DefaultEvaluator, rng, *depth, *maxPlies, *handSize)
		}
		for _, r := range records {
			if err := enc.Encode(r); err != nil {
				fmt.Fprintln(os.Stderr, "nnue-selfplay: encoding record:", err)
				os.Exit(1)
			}
		}
		totalRecords += len(records)
		if (g+1)%10 == 0 || g+1 == *games {
			fmt.Fprintf(os.Stderr, "nnue-selfplay: %d/%d games, %d positions recorded so far\n", g+1, *games, totalRecords)
		}
	}
}
