package actions

import (
	"testing"

	"github.com/chess404/realtime/internal/engine/core"
)

// TestCastlingRefusedAfterItsRookIsFusedAway is a permanent regression
// test for a real corruption bug, root-caused via a full self-play run's
// action trail: p.castling is an incrementally-maintained bitmask,
// cleared only by castlingRightsClearedBy inside core.MakeMove for
// ordinary chess moves. Fusion (ApplyCardAction -> applyFusion) can
// replace the piece sitting on a rook's home square with a Queen (the
// bishop+rook-becomes-queen special case) without ever touching
// p.castling, since it's a card effect, not a chess move. A real crash
// traced to exactly this: a player fused a bishop with its OWN kingside
// rook into a queen, keeping kingside rights nominally set; a later
// castle blindly moved "the rook" via core's hardcoded corner squares,
// XORing bitboard bits for a piece that was no longer there and leaving
// two different piece-type bitboards claiming the same square (found by
// a bitboard consistency check: Queen and the phantom Rook both claiming
// h8). Fixed in core/overlays_movegen.go's generateCastlesOverlay by
// re-verifying piece identity at the point of use -- matching how
// internal/match's own castling-rights check re-derives eligibility from
// current board state every time, never trusting a cached bit alone.
func TestCastlingRefusedAfterItsRookIsFusedAway(t *testing.T) {
	// White's kingside rook (h1) will be fused away; the queenside rook
	// (a1) stays untouched as a control, confirming the fix is scoped to
	// the actual affected side, not a blanket "castling never works after
	// any fusion" overcorrection.
	p := core.MustParseFEN("4k3/8/8/8/8/8/6B1/R3K2R w KQ - 0 1")
	ov := core.NewCardOverlay()

	bishopSq := core.NewSquare(6, 1) // g2
	rookSq := core.NewSquare(7, 0)   // h1
	kingFrom := core.NewSquare(4, 0)
	kingsideTo := core.NewSquare(6, 0)
	queensideTo := core.NewSquare(2, 0)

	before := core.GenerateLegalMovesWithOverlay(p, ov)
	if !hasCastleMove(before, kingFrom, kingsideTo) {
		t.Fatal("test setup: expected White to be able to castle kingside before fusion")
	}
	if !hasCastleMove(before, kingFrom, queensideTo) {
		t.Fatal("test setup: expected White to be able to castle queenside before fusion")
	}

	undo := ApplyCardAction(p, ov, Action{
		Kind: ActionCard,
		Card: CardInstance{ID: "c1", Mechanic: MechanicFullFusion},
		Targets: CardTargets{
			NumTargets: 2,
			First:      bishopSq,
			Second:     rookSq,
		},
	})
	defer UndoCardAction(p, ov, undo)

	if got := p.PieceAt(rookSq); got.Type != core.Queen || got.Color != core.White {
		t.Fatalf("test setup: expected the bishop+rook fusion to leave a white queen on h1, got %+v", got)
	}
	if !p.HasCastleRight(core.CastleWhiteKingside) {
		t.Fatal("test setup: expected kingside castling rights to still be nominally set (that's the bug this test guards against silently masking)")
	}

	after := core.GenerateLegalMovesWithOverlay(p, ov)
	if hasCastleMove(after, kingFrom, kingsideTo) {
		t.Error("expected kingside castling to be refused once its own rook was fused away, got it as a legal move")
	}
	if !hasCastleMove(after, kingFrom, queensideTo) {
		t.Error("expected queenside castling (untouched rook) to remain legal")
	}

	// The real crash: actually applying the (should-be-impossible) castle
	// used to corrupt the board. Confirm it's simply not offered, so
	// there's nothing for a caller to even try applying.
	for _, m := range after {
		if m.Flag == core.CastleKingside {
			t.Fatalf("expected no CastleKingside move in the legal set, found %+v", m)
		}
	}
}

func hasCastleMove(moves []core.Move, from, to core.Square) bool {
	for _, m := range moves {
		if m.From == from && m.To == to && (m.Flag == core.CastleKingside || m.Flag == core.CastleQueenside) {
			return true
		}
	}
	return false
}
