package nnue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// goldenFeatureCase is one exported (position, hand/overlay summary) pair
// together with the exact feature index set ActiveFeatures computes for
// it -- the cross-language contract pytrainer/test_roundtrip.py checks
// against. FEN + counts is everything a re-implementation needs: per
// features.go, the card-aware half of the feature set only ever consumes
// COUNTS (hand length, countWhere's tally), never which exact squares are
// frozen/shielded/fortressed.
type goldenFeatureCase struct {
	Name          string `json:"name"`
	FEN           string `json:"fen"`
	WhiteHandSize int    `json:"whiteHandSize"`
	BlackHandSize int    `json:"blackHandSize"`
	FrozenWhite   int    `json:"frozenWhite"`
	FrozenBlack   int    `json:"frozenBlack"`
	ShieldedWhite int    `json:"shieldedWhite"`
	ShieldedBlack int    `json:"shieldedBlack"`
	FortressWhite bool   `json:"fortressWhite"`
	FortressBlack bool   `json:"fortressBlack"`
	Features      []int  `json:"features"`
}

func handOfSize(n int) actions.Hand {
	h := make(actions.Hand, n)
	for i := range h {
		h[i] = actions.CardInstance{ID: "x", Mechanic: actions.MechanicFreeze}
	}
	return h
}

func countOverlayFor(p *core.Position, c core.Color, has func(core.Square) bool) int {
	count := 0
	pieces := p.Occupied(c)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		if has(sq) {
			count++
		}
	}
	return count
}

// buildGoldenCases constructs representative (position, overlay) scenarios
// covering every branch ActiveFeatures takes: all 4 king-bucket quadrants,
// every count-bucket boundary (0, 1, 2-or-more) for hand size on both
// colors, and frozen/shielded counts crossing the same boundaries combined
// with every fortress on/off pairing.
func buildGoldenCases() []goldenFeatureCase {
	var cases []goldenFeatureCase

	add := func(name string, p *core.Position, ov *core.CardOverlay, whiteHandSize, blackHandSize int) {
		wh, bh := handOfSize(whiteHandSize), handOfSize(blackHandSize)
		feats := ActiveFeatures(p, ov, wh, bh)
		sorted := append([]int(nil), feats...)
		sort.Ints(sorted)
		cases = append(cases, goldenFeatureCase{
			Name: name, FEN: p.ToFEN(),
			WhiteHandSize: whiteHandSize, BlackHandSize: blackHandSize,
			FrozenWhite:   countOverlayFor(p, core.White, ov.IsFrozen),
			FrozenBlack:   countOverlayFor(p, core.Black, ov.IsFrozen),
			ShieldedWhite: countOverlayFor(p, core.White, ov.IsShielded),
			ShieldedBlack: countOverlayFor(p, core.Black, ov.IsShielded),
			FortressWhite: ov.HasFortress(core.White),
			FortressBlack: ov.HasFortress(core.Black),
			Features:      sorted,
		})
	}

	add("starting_position", core.NewStartingPosition(), core.NewCardOverlay(), 0, 0)

	// One king per quadrant (kingBucket depends only on White's own king
	// square), with a scattering of every non-king piece type/color so each
	// bucket's piece-square feature arithmetic gets exercised.
	quadrantKings := []core.Square{
		core.NewSquare(0, 0), // a1 -> bucket 0 (file<4, rank<4)
		core.NewSquare(7, 0), // h1 -> bucket 1 (file>=4, rank<4)
		core.NewSquare(0, 7), // a8 -> bucket 2 (file<4, rank>=4)
		core.NewSquare(7, 7), // h8 -> bucket 3 (file>=4, rank>=4)
	}
	for i, wk := range quadrantKings {
		p := core.NewEmptyPosition()
		p.SetPiece(wk, core.Piece{Type: core.King, Color: core.White})
		p.SetPiece(core.NewSquare(4, 3), core.Piece{Type: core.King, Color: core.Black})
		p.SetPiece(core.NewSquare(1, 1), core.Piece{Type: core.Pawn, Color: core.White})
		p.SetPiece(core.NewSquare(2, 2), core.Piece{Type: core.Knight, Color: core.Black})
		p.SetPiece(core.NewSquare(3, 3), core.Piece{Type: core.Bishop, Color: core.White})
		p.SetPiece(core.NewSquare(5, 5), core.Piece{Type: core.Rook, Color: core.Black})
		p.SetPiece(core.NewSquare(6, 6), core.Piece{Type: core.Queen, Color: core.White})
		add(fmt.Sprintf("king_bucket_%d", i), p, core.NewCardOverlay(), 0, 0)
	}

	// Hand-size count-bucket boundaries: 0, 1, 2, 5 (2 and 5 must land in
	// the same "2-or-more" bucket).
	base := core.NewStartingPosition()
	for _, wh := range []int{0, 1, 2, 5} {
		for _, bh := range []int{0, 1, 2, 5} {
			add(fmt.Sprintf("hand_sizes_w%d_b%d", wh, bh), base, core.NewCardOverlay(), wh, bh)
		}
	}

	// Frozen/shielded counts (white=2 -> "2-or-more", black=1 -> exactly
	// 1) crossed with every fortress on/off combination.
	buildOverlayPosition := func() *core.Position {
		p := core.NewEmptyPosition()
		p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
		p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
		p.SetPiece(core.NewSquare(0, 1), core.Piece{Type: core.Pawn, Color: core.White})
		p.SetPiece(core.NewSquare(1, 1), core.Piece{Type: core.Pawn, Color: core.White})
		p.SetPiece(core.NewSquare(2, 1), core.Piece{Type: core.Pawn, Color: core.White})
		p.SetPiece(core.NewSquare(0, 6), core.Piece{Type: core.Pawn, Color: core.Black})
		p.SetPiece(core.NewSquare(1, 6), core.Piece{Type: core.Pawn, Color: core.Black})
		p.SetPiece(core.NewSquare(2, 6), core.Piece{Type: core.Pawn, Color: core.Black})
		return p
	}
	for _, fw := range []bool{false, true} {
		for _, fb := range []bool{false, true} {
			p := buildOverlayPosition()
			ov := core.NewCardOverlay()
			ov.SetFrozen(core.NewSquare(0, 1), true)
			ov.SetFrozen(core.NewSquare(1, 1), true) // white: 2 frozen
			ov.SetFrozen(core.NewSquare(0, 6), true) // black: 1 frozen
			ov.SetShielded(core.NewSquare(2, 1), 999)
			if fw {
				ov.SetFortress(core.White, core.NewSquare(3, 3), 999)
			}
			if fb {
				ov.SetFortress(core.Black, core.NewSquare(4, 4), 999)
			}
			add(fmt.Sprintf("overlay_fw%v_fb%v", fw, fb), p, ov, 3, 2)
		}
	}

	return cases
}

// TestGenerateNNUEFeatureFixtures regenerates testdata/golden_features.json
// from the CURRENT ActiveFeatures implementation every time it runs (a
// golden file that can never drift out of sync with the Go code, since Go
// itself produces it fresh on every test run) and sanity-checks each case
// before writing it out: no duplicate indices, every index in
// [0, NumFeatures), and a chess-feature count matching the piece count on
// the board. pytrainer/test_roundtrip.py is the actual cross-language
// verification -- it reads this file, recomputes every case's features
// independently in Python, and asserts the exact same index SET results;
// see that file for the other half of Task 8's round-trip requirement.
func TestGenerateNNUEFeatureFixtures(t *testing.T) {
	cases := buildGoldenCases()

	for _, c := range cases {
		seen := make(map[int]bool, len(c.Features))
		nonKingPieces := 0
		p, err := core.ParseFEN(c.FEN)
		if err != nil {
			t.Fatalf("case %s: FEN %q failed to re-parse: %v", c.Name, c.FEN, err)
		}
		for sq := core.Square(0); sq < 64; sq++ {
			piece := p.PieceAt(sq)
			if !piece.IsNone() && piece.Type != core.King {
				nonKingPieces++
			}
		}

		for _, f := range c.Features {
			if f < 0 || f >= NumFeatures {
				t.Errorf("case %s: feature index %d out of range [0, %d)", c.Name, f, NumFeatures)
			}
			if seen[f] {
				t.Errorf("case %s: duplicate feature index %d", c.Name, f)
			}
			seen[f] = true
		}

		chessFeatureCount := 0
		for _, f := range c.Features {
			if f < numChessFeatures {
				chessFeatureCount++
			}
		}
		if chessFeatureCount != nonKingPieces {
			t.Errorf("case %s: expected %d chess (piece-square) features, got %d", c.Name, nonKingPieces, chessFeatureCount)
		}
	}

	out, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatalf("marshaling golden cases: %v", err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("creating testdata dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join("testdata", "golden_features.json"), out, 0o644); err != nil {
		t.Fatalf("writing golden_features.json: %v", err)
	}
}
