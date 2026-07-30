// Command nnue-selfplay generates self-play games -- genuinely playing
// cards, not just dealing them, see search.GenerateSelfPlayGame -- and
// writes one JSON line per recorded position to stdout or a file, in
// exactly the field set nnue/pytrainer/train.py consumes to build a
// training set for Phase 3's NNUE (Task 8).
//
// Example:
//
//	go run ./cmd/nnue-selfplay -games 200 -out selfplay.jsonl
//	python internal/engine/nnue/pytrainer/train.py selfplay.jsonl trained.bin
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"

	"github.com/chess404/realtime/internal/engine/search"
)

func main() {
	games := flag.Int("games", 200, "number of self-play games to generate")
	depth := flag.Int("depth", 2, "search depth per move")
	maxPlies := flag.Int("plies", 60, "maximum plies per game")
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

	totalRecords := 0
	for g := 0; g < *games; g++ {
		records := search.GenerateSelfPlayGame(search.DefaultEvaluator, rng, *depth, *maxPlies, *handSize)
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
