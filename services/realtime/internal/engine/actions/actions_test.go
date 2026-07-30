package actions

import (
	"testing"

	"github.com/chess404/realtime/internal/engine/core"
)

func kings(p *core.Position, whiteSq, blackSq core.Square) {
	p.SetPiece(whiteSq, core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(blackSq, core.Piece{Type: core.King, Color: core.Black})
}

func targetsOf(actions []Action) map[core.Square]bool {
	out := map[core.Square]bool{}
	for _, a := range actions {
		out[a.Targets.First] = true
	}
	return out
}

// -- Freeze / Shield -----------------------------------------------------

func TestFreezeCandidatesOnlyTargetEnemyNonKing(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Rook, Color: core.Black})
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})

	got := targetsOf(freezeCandidates(p, CardInstance{ID: "c1", Mechanic: MechanicFreeze}))
	if len(got) != 1 || !got[core.NewSquare(0, 7)] {
		t.Fatalf("expected only the enemy rook (a8) as a candidate, got %v", got)
	}
}

func TestShieldCandidatesOnlyTargetOwnNonKing(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Rook, Color: core.Black})
	ov := core.NewCardOverlay()

	got := targetsOf(shieldCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicShield}))
	if len(got) != 1 || !got[core.NewSquare(0, 0)] {
		t.Fatalf("expected only the own rook (a1) as a candidate, got %v", got)
	}
}

func TestShieldPrioritizesAttackedPieces(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	// White rook a1 is undefended and NOT attacked; white knight b1 IS
	// attacked by the black rook on b8.
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(1, 0), core.Piece{Type: core.Knight, Color: core.White})
	p.SetPiece(core.NewSquare(1, 7), core.Piece{Type: core.Rook, Color: core.Black})
	ov := core.NewCardOverlay()

	got := shieldCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicShield})
	if len(got) == 0 || got[0].Targets.First != core.NewSquare(1, 0) {
		t.Fatalf("expected the attacked knight (b1) ranked first, got %+v", got)
	}
}

// -- Fortress / Lavaground / Unabomber -----------------------------------

func TestFortressCandidatesAreClampedToValidAnchors(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(0, 0), core.NewSquare(7, 7))

	for _, a := range fortressCandidates(p, CardInstance{ID: "c1", Mechanic: MechanicFortress}) {
		anchor := a.Targets.First
		if anchor.File() < 0 || anchor.File() > 6 || anchor.Rank() < 0 || anchor.Rank() > 6 {
			t.Fatalf("fortress anchor %v out of the valid [0,6]x[0,6] range", anchor)
		}
	}
}

func TestLavagroundCandidatesSkipOccupiedAndExistingLava(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(0, 0), core.NewSquare(7, 7))
	occupied := core.NewSquare(3, 3)
	p.SetPiece(occupied, core.Piece{Type: core.Pawn, Color: core.White})
	ov := core.NewCardOverlay()
	alreadyLava := core.NewSquare(3, 4)
	ov.AddLava(alreadyLava, 2)

	got := targetsOf(lavagroundCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicLavaground}))
	if got[occupied] {
		t.Error("expected an occupied square to never be a lavaground candidate")
	}
	if got[alreadyLava] {
		t.Error("expected a square that already has lava to never be a candidate")
	}
}

func TestUnabomberCandidatesOnlyTargetOwnNonKing(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Rook, Color: core.Black})

	got := targetsOf(unabomberCandidates(p, CardInstance{ID: "c1", Mechanic: MechanicUnabomber}))
	if len(got) != 1 || !got[core.NewSquare(0, 0)] {
		t.Fatalf("expected only the own rook (a1) as a candidate, got %v", got)
	}
}

// -- BlackHole -------------------------------------------------------------

func TestBlackholeCandidatesAreDistinctSquarePairs(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(0, 0), core.NewSquare(7, 7))

	for _, a := range blackholeCandidates(p, CardInstance{ID: "c1", Mechanic: MechanicBlackhole}) {
		if a.Targets.NumTargets != 2 {
			t.Fatalf("expected NumTargets=2, got %d", a.Targets.NumTargets)
		}
		if a.Targets.First == a.Targets.Second {
			t.Fatalf("expected two distinct squares, got %v twice", a.Targets.First)
		}
	}
}

// -- Fusion ----------------------------------------------------------------

func TestFusionCandidatesRequireAdjacency(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Knight, Color: core.White}) // a1
	p.SetPiece(core.NewSquare(7, 0), core.Piece{Type: core.Rook, Color: core.White})   // h1, far away
	ov := core.NewCardOverlay()

	got := fusionCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicFullFusion}, false)
	if len(got) != 0 {
		t.Fatalf("expected zero candidates for two non-adjacent pieces, got %d", len(got))
	}
}

func TestFusionCandidatesRejectRedundantPairs(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	p.SetPiece(core.NewSquare(3, 0), core.Piece{Type: core.Knight, Color: core.White}) // d1
	p.SetPiece(core.NewSquare(3, 1), core.Piece{Type: core.Knight, Color: core.White}) // d2, adjacent, same type
	ov := core.NewCardOverlay()

	got := fusionCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicFullFusion}, false)
	if len(got) != 0 {
		t.Fatalf("expected same-type fusion (knight+knight) to be rejected, got %d candidates", len(got))
	}
}

func TestFusionCandidatesRejectAlreadyFusedPieces(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	fused := core.NewSquare(3, 0)
	p.SetPiece(fused, core.Piece{Type: core.Bishop, Color: core.White})
	p.SetPiece(core.NewSquare(3, 1), core.Piece{Type: core.Knight, Color: core.White})
	ov := core.NewCardOverlay()
	ov.SetFused(fused, core.Rook)

	for _, a := range fusionCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicFullFusion}, false) {
		if a.Targets.First == fused || a.Targets.Second == fused {
			t.Fatalf("expected the already-fused piece to never appear as a candidate, got %+v", a)
		}
	}
}

// TestHalfFuseCapRejectsExpensivePairsButFullFusionAllowsThem locks in the
// one real asymmetry between the two mechanics: HalfFuse caps combined
// value at 6 (match_cards.go:1025,1038-1039,1077-1079); FullFusion has no
// such cap.
func TestHalfFuseCapRejectsExpensivePairsButFullFusionAllowsThem(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	rook := core.NewSquare(3, 0)   // value 5
	knight := core.NewSquare(3, 1) // value 3, combined = 8 > cap of 6
	p.SetPiece(rook, core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(knight, core.Piece{Type: core.Knight, Color: core.White})
	ov := core.NewCardOverlay()

	halfFuse := fusionCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicHalfFuse}, true)
	for _, a := range halfFuse {
		if a.Targets.First == rook || a.Targets.Second == rook {
			t.Fatalf("expected HalfFuse to reject the rook+knight pair (combined value 8 > cap 6), got %+v", a)
		}
	}

	fullFusion := fusionCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicFullFusion}, false)
	found := false
	for _, a := range fullFusion {
		if (a.Targets.First == rook && a.Targets.Second == knight) || (a.Targets.First == knight && a.Targets.Second == rook) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected FullFusion to allow the same rook+knight pair (no value cap)")
	}
}

func TestFusionCandidatesAllowBishopRookExceptionAboveTheCap(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	bishop := core.NewSquare(3, 0) // value 3
	rook := core.NewSquare(3, 1)   // value 5, combined = 8 > cap of 6, but bishop+rook is exempt
	p.SetPiece(bishop, core.Piece{Type: core.Bishop, Color: core.White})
	p.SetPiece(rook, core.Piece{Type: core.Rook, Color: core.White})
	ov := core.NewCardOverlay()

	got := fusionCandidates(p, ov, CardInstance{ID: "c1", Mechanic: MechanicHalfFuse}, true)
	found := false
	for _, a := range got {
		if (a.Targets.First == bishop && a.Targets.Second == rook) || (a.Targets.First == rook && a.Targets.Second == bishop) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the bishop+rook pair to be exempt from HalfFuse's value cap")
	}
}

// -- Apply/Undo --------------------------------------------------------

func TestApplyAndUndoFreeze(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	target := core.NewSquare(0, 7)
	p.SetPiece(target, core.Piece{Type: core.Rook, Color: core.Black})
	ov := core.NewCardOverlay()

	a := Action{Kind: ActionCard, Card: CardInstance{ID: "c1", Mechanic: MechanicFreeze}, Targets: CardTargets{NumTargets: 1, First: target}}
	u := ApplyCardAction(p, ov, a)
	if !ov.IsFrozen(target) {
		t.Fatal("expected the target to be frozen after applying Freeze")
	}
	UndoCardAction(p, ov, u)
	if ov.IsFrozen(target) {
		t.Fatal("expected Frozen to be undone")
	}
}

func TestApplyAndUndoFusionRemovesFirstPieceAndTagsSecond(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	first := core.NewSquare(3, 0)  // knight, consumed
	second := core.NewSquare(3, 1) // bishop, survives fused
	p.SetPiece(first, core.Piece{Type: core.Knight, Color: core.White})
	p.SetPiece(second, core.Piece{Type: core.Bishop, Color: core.White})
	ov := core.NewCardOverlay()

	a := Action{Kind: ActionCard, Card: CardInstance{ID: "c1", Mechanic: MechanicFullFusion}, Targets: CardTargets{NumTargets: 2, First: first, Second: second}}
	u := ApplyCardAction(p, ov, a)

	if !p.PieceAt(first).IsNone() {
		t.Error("expected the first piece to be removed from the board")
	}
	if p.PieceAt(second).Type != core.Bishop || ov.FusedWith(second) != core.Knight {
		t.Errorf("expected the second piece to remain a bishop fused with knight, got type=%v fusedWith=%v", p.PieceAt(second).Type, ov.FusedWith(second))
	}

	UndoCardAction(p, ov, u)
	if p.PieceAt(first).Type != core.Knight {
		t.Error("expected the first piece restored after undo")
	}
	if ov.FusedWith(second) != core.NoPieceType {
		t.Error("expected the fusion tag removed after undo")
	}
}

func TestApplyFusionBishopRookBecomesQueen(t *testing.T) {
	p := core.NewEmptyPosition()
	kings(p, core.NewSquare(4, 0), core.NewSquare(4, 7))
	bishop := core.NewSquare(3, 0)
	rook := core.NewSquare(3, 1)
	p.SetPiece(bishop, core.Piece{Type: core.Bishop, Color: core.White})
	p.SetPiece(rook, core.Piece{Type: core.Rook, Color: core.White})
	ov := core.NewCardOverlay()

	a := Action{Kind: ActionCard, Card: CardInstance{ID: "c1", Mechanic: MechanicHalfFuse}, Targets: CardTargets{NumTargets: 2, First: bishop, Second: rook}}
	ApplyCardAction(p, ov, a)

	if !p.PieceAt(bishop).IsNone() {
		t.Error("expected the bishop square emptied")
	}
	survivor := p.PieceAt(rook)
	if survivor.Type != core.Queen || ov.FusedWith(rook) != core.NoPieceType {
		t.Errorf("expected a plain queen with no fusion tag, got type=%v fusedWith=%v", survivor.Type, ov.FusedWith(rook))
	}
}

// -- Turn model / GenerateActions -----------------------------------------

func TestGenerateActionsRespectsAllowCard(t *testing.T) {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	hand := Hand{{ID: "c1", Mechanic: MechanicFreeze}}

	withCard := GenerateActions(p, ov, hand, true)
	hasCard := false
	for _, a := range withCard {
		if a.Kind == ActionCard {
			hasCard = true
		}
	}
	if !hasCard {
		t.Fatal("expected at least one card action when allowCard=true (freeze has enemy non-king targets at the start position)")
	}

	withoutCard := GenerateActions(p, ov, hand, false)
	for _, a := range withoutCard {
		if a.Kind == ActionCard {
			t.Fatal("expected zero card actions when allowCard=false")
		}
	}
}

// -- Terminal status -----------------------------------------------------

func TestTerminalStatusSuppressedWhileHoldingAnyCard(t *testing.T) {
	// Fool's-mate-shaped stalemate/checkmate scenario isn't needed here --
	// reuse the same frozen-knight-is-the-only-mobile-piece shape from
	// core's own test, but this time check the CARD suppression layer:
	// with a card in hand, even a real Checkmate/Stalemate verdict must
	// report Ongoing.
	p := core.NewEmptyPosition()
	whiteKing := core.NewSquare(0, 0)
	p.SetPiece(whiteKing, core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(2, 2), core.Piece{Type: core.Knight, Color: core.Black}) // covers a2
	p.SetPiece(core.NewSquare(3, 2), core.Piece{Type: core.Knight, Color: core.Black}) // covers b2
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	ov := core.NewCardOverlay()

	if got := TerminalStatus(p, ov, nil); got != core.Stalemate {
		t.Fatalf("test setup error: expected a real Stalemate with no cards in hand, got %v", got)
	}
	hand := Hand{{ID: "c1", Mechanic: MechanicFreeze}}
	if got := TerminalStatus(p, ov, hand); got != core.Ongoing {
		t.Fatalf("expected Ongoing while holding any card (suppression), got %v", got)
	}
}
