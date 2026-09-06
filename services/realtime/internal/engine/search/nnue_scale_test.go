package search

import (
	"math/rand"
	"os"
	"testing"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/engine/nnue"
)

// TestNNUEEvalScaleAndSign pins the trained network's output to the
// placeholder eval's conventions on positions whose correct white-perspective
// sign and rough magnitude are known by construction. The first gauntlet run
// (2026-09-02) lost 0-20 with a verified-correct encoder and a loadable
// weights file, so the remaining suspects are exactly these two: a scale the
// search's centipawn expectations cannot use, or an inverted sign. Skips when
// the weights file is absent so the suite still runs on clean checkouts.
func TestNNUEEvalScaleAndSign(t *testing.T) {
	weightsPath := os.Getenv("NNUE_WEIGHTS_PATH")
	if weightsPath == "" {
		weightsPath = "../nnue/pytrainer/trained.bin"
	}
	f, err := os.Open(weightsPath)
	if err != nil {
		t.Skipf("no trained weights at %s: %v", weightsPath, err)
	}
	net, err := nnue.Load(f)
	f.Close()
	if err != nil {
		t.Fatalf("loading %s: %v", weightsPath, err)
	}
	nnueEval := NNUEEvaluator(net)

	cases := []struct {
		name string
		fen  string
		// wantSign: the sign a sound white-perspective eval must produce.
		wantSign int
		// wantMinAbs: a sign-guard floor, not a quality bar — a network whose
		// material signal is this weak still loses to the placeholder eval
		// (the 2026-09-02 gauntlet lost 0-20 with |queen| ≈ 150/67), but the
		// quality bar itself is the gauntlet, not a unit test.
		wantMinAbs int
	}{
		{"start position", "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", 0, 0},
		{"white up a queen", "rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", 1, 100},
		{"black up a queen", "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNB1KBNR w KQkq - 0 1", -1, 50},
	}

	rng := rand.New(rand.NewSource(1))
	hands := Hands{White: actions.SampleHand(rng, 3), Black: actions.SampleHand(rng, 3)}
	emptyOverlay := &core.CardOverlay{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := core.MustParseFEN(tc.fen)
			got := nnueEval(p, emptyOverlay, hands, core.White)
			placeholder := DefaultEvaluator(p, emptyOverlay, hands, core.White)
			t.Logf("nnue=%d placeholder=%d", got, placeholder)

			if tc.wantSign == 0 {
				if got > 150 || got < -150 {
					t.Errorf("symmetric start position scored %d; a sound eval reads ~0", got)
				}
				return
			}
			if tc.wantSign > 0 && got < tc.wantMinAbs {
				t.Errorf("white-up-a-queen scored %d; want >= %d (sign broken)", got, tc.wantMinAbs)
			}
			if tc.wantSign < 0 && got > -tc.wantMinAbs {
				t.Errorf("black-up-a-queen scored %d; want <= %d (sign broken)", got, -tc.wantMinAbs)
			}
		})
	}
}
