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

// TestFusionCandidatesExcludesADiscoveredSelfCheck is a regression test for
// a bug xgauntlet's E0 cross-engine gauntlet found: a real
// ComputerOpponent-vs-NewEngineAdapter game had this package propose a
// fullfusion pair that internal/match correctly rejected with "fullfusion
// would leave a king in check" (match_cards.go:1159-1160's
// kingsRemainSafeWithFusion check). fusionCandidates had no equivalent
// filter at all -- unlike every other mechanic in this package, whose
// candidate generation mirrors the reference's validation, not just its
// heuristic ranking.
//
// White king e1, white knight e2 (blocking a black rook on e8 along the
// e-file), white bishop d3 (adjacent to e2, a valid fusion partner). Fusing
// away the knight on e2 (first, consumed) into the bishop on d3 (second,
// survives) opens the e-file and exposes White's own king to the black
// rook -- exactly the discovered-self-check kingsRemainSafeWithFusion exists
// to catch.
func TestFusionCandidatesExcludesADiscoveredSelfCheck(t *testing.T) {
	p := core.NewEmptyPosition()
	whiteKing := core.NewSquare(4, 0) // e1
	knightSq := core.NewSquare(4, 1)  // e2
	bishopSq := core.NewSquare(3, 2)  // d3
	blackKing := core.NewSquare(0, 7) // a8
	blackRook := core.NewSquare(4, 7) // e8
	p.SetPiece(whiteKing, core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(knightSq, core.Piece{Type: core.Knight, Color: core.White})
	p.SetPiece(bishopSq, core.Piece{Type: core.Bishop, Color: core.White})
	p.SetPiece(blackKing, core.Piece{Type: core.King, Color: core.Black})
	p.SetPiece(blackRook, core.Piece{Type: core.Rook, Color: core.Black})
	ov := core.NewCardOverlay()

	if core.InCheckWithFusion(p, ov, core.White) {
		t.Fatal("test setup error: white should not start in check")
	}

	card := CardInstance{ID: "c1", Mechanic: MechanicFullFusion}
	candidates := generateCardActions(p, ov, card)

	for _, a := range candidates {
		if a.Targets.First == knightSq && a.Targets.Second == bishopSq {
			t.Fatalf("fusionCandidates proposed removing the e2 knight into the d3 bishop, which discovers check on White's own king -- internal/match would reject this with %q", "fullfusion would leave a king in check")
		}
	}

	// Sanity: prove the exclusion is real by directly applying the excluded
	// pair and confirming it does in fact leave White in check (otherwise
	// this test would be vacuously passing because the pair was never a
	// candidate for some unrelated reason, e.g. adjacency or redundancy).
	undo := ApplyCardAction(p, ov, Action{
		Kind: ActionCard, Card: card,
		Targets: CardTargets{NumTargets: 2, First: knightSq, Second: bishopSq},
	})
	inCheck := core.InCheckWithFusion(p, ov, core.White)
	UndoCardAction(p, ov, undo)
	if !inCheck {
		t.Fatal("test setup error: the excluded fusion pair does not actually discover check -- fixture is wrong, not the filter")
	}
}
