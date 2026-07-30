package actions

import (
	"testing"

	"github.com/chess404/realtime/internal/engine/core"
)

// checkConsistency confirms every piece-type bitboard is pairwise
// disjoint and their union matches Occupied/OccupiedAll -- the invariant
// a real corruption bug violated (see move_test.go's
// TestMakeMoveIgnoresPawnOnlyFlagsForANonPawnMover for the actual root
// cause, found by first using this exact check to confirm applyFusion
// itself was clean, narrowing the bug down to core/move.go instead).
func checkConsistency(t *testing.T, p *core.Position, label string) {
	t.Helper()
	var union, whiteUnion, blackUnion core.Bitboard
	for _, pt := range []core.PieceType{core.Pawn, core.Knight, core.Bishop, core.Rook, core.Queen, core.King} {
		for _, c := range []core.Color{core.White, core.Black} {
			bb := p.PieceBitboard(pt, c)
			if bb&union != 0 {
				t.Fatalf("%s: piece type=%v color=%v overlaps an already-claimed square (bits %016x); FEN=%q", label, pt, c, uint64(bb&union), p.ToFEN())
			}
			union |= bb
			if c == core.White {
				whiteUnion |= bb
			} else {
				blackUnion |= bb
			}
		}
	}
	if union != p.OccupiedAll() {
		t.Fatalf("%s: union of piece bitboards %016x != OccupiedAll() %016x; FEN=%q", label, uint64(union), uint64(p.OccupiedAll()), p.ToFEN())
	}
	if whiteUnion != p.Occupied(core.White) {
		t.Fatalf("%s: union of white piece bitboards %016x != Occupied(White) %016x; FEN=%q", label, uint64(whiteUnion), uint64(p.Occupied(core.White)), p.ToFEN())
	}
	if blackUnion != p.Occupied(core.Black) {
		t.Fatalf("%s: union of black piece bitboards %016x != Occupied(Black) %016x; FEN=%q", label, uint64(blackUnion), uint64(p.Occupied(core.Black)), p.ToFEN())
	}
}

// TestApplyFusionBishopRookBothOrderingsStayConsistent covers the
// bishop+rook-becomes-queen special case (applyFusion's isBishopRook
// branch, the most complex of the two -- it removes BOTH squares'
// original occupants and places a fresh piece) in both possible argument
// orderings.
func TestApplyFusionBishopRookBothOrderingsStayConsistent(t *testing.T) {
	cases := []struct {
		name         string
		firstType    core.PieceType
		secondType   core.PieceType
		firstSquare  core.Square
		secondSquare core.Square
	}{
		{"bishop-first-rook-second", core.Bishop, core.Rook, core.NewSquare(2, 2), core.NewSquare(3, 3)},
		{"rook-first-bishop-second", core.Rook, core.Bishop, core.NewSquare(2, 2), core.NewSquare(3, 3)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := core.NewEmptyPosition()
			p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.King, Color: core.White})
			p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black})
			p.SetPiece(c.firstSquare, core.Piece{Type: c.firstType, Color: core.White})
			p.SetPiece(c.secondSquare, core.Piece{Type: c.secondType, Color: core.White})
			ov := core.NewCardOverlay()

			checkConsistency(t, p, "before fusion")

			undo := ApplyCardAction(p, ov, Action{
				Kind: ActionCard,
				Card: CardInstance{ID: "c1", Mechanic: MechanicFullFusion},
				Targets: CardTargets{
					NumTargets: 2,
					First:      c.firstSquare,
					Second:     c.secondSquare,
				},
			})
			checkConsistency(t, p, "after fusion, before undo")

			UndoCardAction(p, ov, undo)
			checkConsistency(t, p, "after undo")
		})
	}
}

// TestApplyFusionNonBishopRookStaysConsistent covers the OTHER branch
// (ov.SetFused tag, no piece-type change): the survivor keeps its own
// bitboard entirely unchanged, only the consumed piece is removed.
func TestApplyFusionNonBishopRookStaysConsistent(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black})
	bishopSq := core.NewSquare(2, 2)
	knightSq := core.NewSquare(3, 3)
	p.SetPiece(bishopSq, core.Piece{Type: core.Bishop, Color: core.White})
	p.SetPiece(knightSq, core.Piece{Type: core.Knight, Color: core.White})
	ov := core.NewCardOverlay()

	checkConsistency(t, p, "before fusion")

	undo := ApplyCardAction(p, ov, Action{
		Kind: ActionCard,
		Card: CardInstance{ID: "c1", Mechanic: MechanicFullFusion},
		Targets: CardTargets{
			NumTargets: 2,
			First:      bishopSq,
			Second:     knightSq,
		},
	})
	checkConsistency(t, p, "after fusion, before undo")

	UndoCardAction(p, ov, undo)
	checkConsistency(t, p, "after undo")
}
