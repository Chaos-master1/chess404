package core

import "testing"

// -- Frozen ----------------------------------------------------------------

func TestThawAfterMoveClearsOnlyMoversColor(t *testing.T) {
	p := NewEmptyPosition()
	whiteSq := NewSquare(0, 0)
	blackSq := NewSquare(7, 7)
	p.SetPiece(whiteSq, Piece{Type: Knight, Color: White})
	p.SetPiece(blackSq, Piece{Type: Knight, Color: Black})
	ov := NewCardOverlay()
	ov.SetFrozen(whiteSq, true)
	ov.SetFrozen(blackSq, true)

	ThawAfterMove(p, ov, White)

	if ov.IsFrozen(whiteSq) {
		t.Fatal("expected White's frozen piece to thaw after White completes a move")
	}
	if !ov.IsFrozen(blackSq) {
		t.Fatal("expected Black's frozen piece to remain frozen -- only the mover's own color thaws")
	}
}

// TestFrozenBlocksSubmittableButNotLegalOrTerminalStatus locks in the most
// important Frozen subtlety found in internal/match: the reference's
// checkmate/stalemate classifier (hasLegalMoveWithFusion, chess.go:91-119)
// has zero Frozen references, so a position where the only pseudo-legal
// moves belong to a frozen piece is NOT stalemate on the live server.
func TestFrozenBlocksSubmittableButNotLegalOrTerminalStatus(t *testing.T) {
	p := NewEmptyPosition()
	whiteKing := NewSquare(0, 0)   // a1 -- only 3 neighbors: a2, b1, b2
	whiteKnight := NewSquare(1, 0) // b1
	p.SetPiece(whiteKing, Piece{Type: King, Color: White})
	p.SetPiece(whiteKnight, Piece{Type: Knight, Color: White})
	p.SetPiece(NewSquare(2, 2), Piece{Type: Knight, Color: Black}) // c3: covers a2
	p.SetPiece(NewSquare(3, 2), Piece{Type: Knight, Color: Black}) // d3: covers b2
	p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black})   // e8, uninvolved

	ov := NewCardOverlay()

	if legal := GenerateLegalMovesWithOverlay(p, ov); len(legal) == 0 {
		t.Fatal("test setup error: expected the knight to have legal moves before freezing")
	}
	if status := TerminalStatusWithOverlay(p, ov); status != Ongoing {
		t.Fatalf("test setup error: expected Ongoing before freezing, got %v", status)
	}

	ov.SetFrozen(whiteKnight, true)

	if submittable := GenerateSubmittableMoves(p, ov); len(submittable) != 0 {
		t.Fatalf("expected zero submittable moves once the only mobile piece is frozen, got %v", submittable)
	}
	if status := TerminalStatusWithOverlay(p, ov); status != Ongoing {
		t.Fatalf("expected TerminalStatusWithOverlay to stay Ongoing (Frozen-blind, matching internal/match), got %v", status)
	}
	if legal := GenerateLegalMovesWithOverlay(p, ov); len(legal) == 0 {
		t.Fatal("expected GenerateLegalMovesWithOverlay to still report the frozen knight's moves (Frozen-blind)")
	}
}

// -- Shielded ----------------------------------------------------------

func TestShieldConsumedOnce(t *testing.T) {
	ov := NewCardOverlay()
	sq := NewSquare(4, 4)
	if ov.TryConsumeShield(sq) {
		t.Fatal("expected no shield before SetShielded")
	}
	ov.SetShielded(sq, 10)
	if !ov.IsShielded(sq) {
		t.Fatal("expected IsShielded true after SetShielded")
	}
	if !ov.TryConsumeShield(sq) {
		t.Fatal("expected TryConsumeShield to consume an active shield")
	}
	if ov.TryConsumeShield(sq) {
		t.Fatal("expected the shield to be gone after being consumed once")
	}
}

// TestShieldExpiryAsymmetryMatchesReference locks in a real, verified
// asymmetry in internal/match (match_cards.go's ShieldTurn=FullMoveNum+1,
// checked in cleanupTemporaryEffects): a shield cast on White's turn
// protects through Black's one reply; a shield cast on Black's turn expires
// before White ever gets a turn to test it, because Black's own move within
// that same turn already bumps FullMoveNum to the threshold.
func TestShieldExpiryAsymmetryMatchesReference(t *testing.T) {
	sq := NewSquare(0, 0)

	castOnWhitesTurn := NewCardOverlay()
	castOnWhitesTurn.SetShielded(sq, 5) // shieldExpiry = 6
	castOnWhitesTurn.ExpireShields(5)   // White's own move: FullMoveNum unchanged
	if !castOnWhitesTurn.IsShielded(sq) {
		t.Fatal("a shield cast while FullMoveNum==5 must survive a cleanup check still at FullMoveNum==5")
	}
	castOnWhitesTurn.ExpireShields(6) // Black's reply bumps FullMoveNum to 6
	if castOnWhitesTurn.IsShielded(sq) {
		t.Fatal("a shield cast on White's turn should expire right after Black's one reply")
	}

	castOnBlacksTurn := NewCardOverlay()
	castOnBlacksTurn.SetShielded(sq, 5) // same cast-time snapshot, shieldExpiry = 6
	castOnBlacksTurn.ExpireShields(6)   // Black's OWN move already bumped FullMoveNum to 6
	if castOnBlacksTurn.IsShielded(sq) {
		t.Fatal("a shield cast on Black's turn should already be expired before White ever gets a turn (the documented asymmetry)")
	}
}

// -- Fortress ------------------------------------------------------------

func TestFortressBlocksNonOwnerLandingAndPathCrossing(t *testing.T) {
	p := NewEmptyPosition()
	rookFrom := NewSquare(0, 0) // a1
	p.SetPiece(rookFrom, Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(3, 1), Piece{Type: King, Color: White}) // d2, off every rook line and not adjacent to e8
	p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black}) // e8, uninvolved

	ov := NewCardOverlay()
	ov.SetFortress(Black, NewSquare(0, 2), 2) // a3-b4, owned by Black

	moves := GenerateLegalMovesWithOverlay(p, ov)
	dest := map[Square]bool{}
	for _, m := range moves {
		if m.From == rookFrom {
			dest[m.To] = true
		}
	}

	if !dest[NewSquare(0, 1)] { // a2: before the wall, must be reachable
		t.Error("expected the rook to reach a2, short of the fortress boundary")
	}
	for _, blocked := range []Square{NewSquare(0, 2), NewSquare(1, 2), NewSquare(0, 3), NewSquare(1, 3)} {
		if dest[blocked] {
			t.Errorf("expected %v (inside the enemy fortress) to be unreachable", blocked)
		}
	}
	if !dest[NewSquare(7, 0)] { // h1: unrelated direction, must be unaffected
		t.Error("expected the rook's rank-1 moves (unrelated to the fortress) to be unaffected")
	}
}

func TestFortressDoesNotBlockItsOwner(t *testing.T) {
	p := NewEmptyPosition()
	rookFrom := NewSquare(0, 7) // a8
	p.SetPiece(rookFrom, Piece{Type: Rook, Color: Black})
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White}) // e1, uninvolved
	p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black}) // e8, uninvolved
	p.sideToMove = Black

	ov := NewCardOverlay()
	ov.SetFortress(Black, NewSquare(0, 2), 2) // a3-b4, owned by Black (the mover)

	moves := GenerateLegalMovesWithOverlay(p, ov)
	reachesA4 := false
	for _, m := range moves {
		if m.From == rookFrom && m.To == NewSquare(0, 3) { // a4, inside its own fortress
			reachesA4 = true
		}
	}
	if !reachesA4 {
		t.Error("expected the fortress's own owner to move freely into/through it")
	}
}

func TestFortressBlocksSliderAttackRayForCheckDetection(t *testing.T) {
	p := NewEmptyPosition()
	rookFrom := NewSquare(0, 0) // a1
	kingSq := NewSquare(0, 4)   // a5, straight up the a-file
	p.SetPiece(rookFrom, Piece{Type: Rook, Color: White})
	p.SetPiece(kingSq, Piece{Type: King, Color: Black})

	if !p.IsAttacked(kingSq, White) {
		t.Fatal("test setup error: expected a5 to be attacked by the rook with no fortress present")
	}

	ov := NewCardOverlay()
	ov.SetFortress(Black, NewSquare(0, 2), 2) // a3-b4, owned by Black (the king's own side)

	if IsAttackedWithFortress(p, ov, kingSq, White) {
		t.Error("expected Black's own fortress to block the rook's ray before it reaches a5")
	}
}

// TestCastlingFortressGap locks in a real, verified gap in internal/match:
// fortressEntryBlocked checks the king's FINAL square like any other move's
// destination, but the pass-through square is only ever checked for
// "attacked" -- being inside a fortress isn't itself "attacked" -- so a king
// CAN legally castle through (not onto) a fortress square.
func TestCastlingFortressGap(t *testing.T) {
	newBoard := func() *Position {
		p := NewEmptyPosition()
		p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White}) // e1
		p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White}) // h1
		p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black}) // e8
		p.castling = CastleWhiteKingside
		return p
	}
	hasKingsideCastle := func(p *Position, ov *CardOverlay) bool {
		for _, m := range GenerateLegalMovesWithOverlay(p, ov) {
			if m.Flag == CastleKingside {
				return true
			}
		}
		return false
	}

	// Fortress at e1-f2 covers the pass-through square (f1) but not the
	// landing square (g1) -- castling must still succeed.
	passThroughFortressed := NewCardOverlay()
	passThroughFortressed.SetFortress(Black, NewSquare(4, 0), 2) // e1,f1,e2,f2
	if !hasKingsideCastle(newBoard(), passThroughFortressed) {
		t.Error("expected castling to succeed despite the pass-through square being inside an enemy fortress")
	}

	// Fortress at g1-h2 covers the landing square (g1) but not the
	// pass-through square (f1) -- castling must be blocked.
	landingFortressed := NewCardOverlay()
	landingFortressed.SetFortress(Black, NewSquare(6, 0), 2) // g1,h1,g2,h2
	if hasKingsideCastle(newBoard(), landingFortressed) {
		t.Error("expected castling to be blocked when the landing square is inside an enemy fortress")
	}
}

// -- Fused/FusedWith -----------------------------------------------------

func TestFusedPieceUnionsMovesFromBothTypes(t *testing.T) {
	p := NewEmptyPosition()
	from := NewSquare(3, 3) // d4
	p.SetPiece(from, Piece{Type: Bishop, Color: White})
	p.SetPiece(NewSquare(1, 0), Piece{Type: King, Color: White}) // b1, off every d4 line
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black}) // a8, off every d4 line

	ov := NewCardOverlay()
	ov.SetFused(from, Rook)

	dest := map[Square]bool{}
	for _, m := range GenerateLegalMovesWithOverlay(p, ov) {
		if m.From == from {
			dest[m.To] = true
		}
	}
	if !dest[NewSquare(3, 7)] { // d8: rook-only, unreachable by a bishop
		t.Error("expected the fused Rook secondary type to reach d8 up the d-file")
	}
	if !dest[NewSquare(6, 6)] { // g7: bishop-only, unreachable by a rook
		t.Error("expected the bishop's own real-type diagonal moves to still be present (union, not replacement)")
	}
	if dest[NewSquare(4, 5)] { // e6: knight-shaped, neither type reaches it
		t.Error("expected no knight-pattern destination to leak in")
	}
}

func TestFusedPieceAttacksViaSecondaryTypeOnly(t *testing.T) {
	p := NewEmptyPosition()
	from := NewSquare(3, 3) // d4
	kingSq := NewSquare(3, 7) // d8 -- reachable by rook pattern, not by bishop
	p.SetPiece(from, Piece{Type: Bishop, Color: Black})
	p.SetPiece(kingSq, Piece{Type: King, Color: White})

	ov := NewCardOverlay()
	ov.SetFused(from, Rook)

	if IsAttackedWithFortress(p, ov, kingSq, Black) {
		t.Error("a bishop's real type alone should not reach d8 from d4 (fortress-aware query is fusion-BLIND by design)")
	}
	if !IsAttackedWithFusion(p, ov, kingSq, Black) {
		t.Error("expected the fused rook-type secondary attack to reach d8 up the d-file")
	}
}

// -- Lava ------------------------------------------------------------------

func TestResolveLavaDestroysLandingOccupantAndTicksOthers(t *testing.T) {
	landing := NewSquare(4, 4) // e5
	other := NewSquare(0, 0)   // a1, unrelated lava square
	p := NewEmptyPosition()
	p.SetPiece(landing, Piece{Type: Knight, Color: Black})
	ov := NewCardOverlay()
	ov.AddLava(landing, 2)
	ov.AddLava(other, 2)

	cleared := ResolveLava(p, ov, landing)
	if len(cleared) != 1 || cleared[0] != landing {
		t.Fatalf("expected the landing square destroyed, got %v", cleared)
	}
	if !p.PieceAt(landing).IsNone() {
		t.Fatal("expected the landing square's occupant to be removed")
	}
	if len(ov.lavaSquares) != 1 || ov.lavaSquares[0].Sq != other || ov.lavaSquares[0].MovesLeft != 1 {
		t.Fatalf("expected the untriggered lava square to tick from 2 to 1 and remain, got %+v", ov.lavaSquares)
	}
}

func TestResolveLavaSparesKingAndConsumesShieldInstead(t *testing.T) {
	landing := NewSquare(4, 4)

	pKing := NewEmptyPosition()
	pKing.SetPiece(landing, Piece{Type: King, Color: Black})
	ovKing := NewCardOverlay()
	ovKing.AddLava(landing, 1)
	if cleared := ResolveLava(pKing, ovKing, landing); cleared != nil {
		t.Fatalf("expected a king to be immune to lava, got cleared=%v", cleared)
	}
	if pKing.PieceAt(landing).IsNone() {
		t.Fatal("the king should still be on the board")
	}

	pShielded := NewEmptyPosition()
	pShielded.SetPiece(landing, Piece{Type: Queen, Color: Black})
	ovShielded := NewCardOverlay()
	ovShielded.AddLava(landing, 1)
	ovShielded.SetShielded(landing, 0)
	if cleared := ResolveLava(pShielded, ovShielded, landing); cleared != nil {
		t.Fatalf("expected a shielded piece to survive lava (shield consumed instead), got cleared=%v", cleared)
	}
	if ovShielded.IsShielded(landing) {
		t.Fatal("expected the shield to be consumed")
	}
	if pShielded.PieceAt(landing).IsNone() {
		t.Fatal("the shielded piece should still be on the board")
	}
}

// -- Bomb --------------------------------------------------------------

func TestResolveBombsDetonatesAtZero(t *testing.T) {
	center := NewSquare(4, 4)   // e5
	neighbor := NewSquare(4, 5) // e6, inside the blast
	outside := NewSquare(0, 0)  // a1, outside the blast

	p := NewEmptyPosition()
	p.SetPiece(center, Piece{Type: Rook, Color: White})
	p.SetPiece(neighbor, Piece{Type: Pawn, Color: Black})
	p.SetPiece(outside, Piece{Type: Pawn, Color: Black})
	ov := NewCardOverlay()
	ov.AddBomb(center, White, 1)

	cleared := ResolveBombs(p, ov)
	if len(cleared) != 2 {
		t.Fatalf("expected the carrier and its one neighbor destroyed, got %v", cleared)
	}
	if !p.PieceAt(center).IsNone() || !p.PieceAt(neighbor).IsNone() {
		t.Fatal("expected both the carrier and its neighbor removed")
	}
	if p.PieceAt(outside).IsNone() {
		t.Fatal("a1 is outside the blast radius and must survive")
	}
	if len(ov.bombTimers) != 0 {
		t.Fatal("expected the spent timer to be dropped")
	}
}

// TestResolveBombsFizzlesIfCarrierWasCapturedFirst matches
// resolveBombEffects' `piece == nil || !piece.Bomb` check
// (match_cards.go:1404-1406): if the tracked square's marker bit is gone
// (the original carrier was replaced and the tracker never re-pointed
// there), the timer fizzles silently instead of destroying whatever now
// occupies the square.
func TestResolveBombsFizzlesIfCarrierWasCapturedFirst(t *testing.T) {
	sq := NewSquare(4, 4)
	p := NewEmptyPosition()
	p.SetPiece(sq, Piece{Type: Knight, Color: Black})
	ov := NewCardOverlay()
	ov.bombTimers = append(ov.bombTimers, BombTimer{Sq: sq, TurnsLeft: 1, Owner: White})
	// bombMarker deliberately left unset at sq, simulating a fresh occupant.

	cleared := ResolveBombs(p, ov)
	if cleared != nil {
		t.Fatalf("expected a fizzle (no marker) to clear nothing, got %v", cleared)
	}
	if p.PieceAt(sq).IsNone() {
		t.Fatal("the replacement piece must survive a fizzled bomb")
	}
}

func TestBombFollowsPieceThroughAMove(t *testing.T) {
	ov := NewCardOverlay()
	from, to := NewSquare(1, 1), NewSquare(1, 3)
	ov.AddBomb(from, White, 2)
	ov.MoveOverlay(from, to)
	if ov.bombTimers[0].Sq != to {
		t.Fatalf("expected the bomb timer to follow the piece to %v, got %v", to, ov.bombTimers[0].Sq)
	}
	if ov.bombMarker.Has(from) || !ov.bombMarker.Has(to) {
		t.Fatal("expected the bomb marker bit to move with the piece")
	}
}

// -- BlackHole ---------------------------------------------------------

func TestTickBlackHolesOpponentGatedCadenceAndDualBlast(t *testing.T) {
	sq1, sq2 := NewSquare(1, 1), NewSquare(6, 6)
	p := NewEmptyPosition()
	p.SetPiece(sq1, Piece{Type: Pawn, Color: Black})
	p.SetPiece(sq2, Piece{Type: Pawn, Color: Black})
	ov := NewCardOverlay()
	ov.AddBlackHole(sq1, sq2, White, 2)

	if cleared := TickBlackHoles(p, ov, White); cleared != nil {
		t.Fatalf("the owner's own move must not tick the countdown, got cleared=%v", cleared)
	}
	if ov.blackHoles[0].TurnsLeft != 2 {
		t.Fatalf("expected TurnsLeft unchanged at 2 after the owner's move, got %d", ov.blackHoles[0].TurnsLeft)
	}

	if cleared := TickBlackHoles(p, ov, Black); cleared != nil || ov.blackHoles[0].TurnsLeft != 1 {
		t.Fatalf("expected one tick down to 1 with no detonation yet, got cleared=%v turnsLeft=%d", cleared, ov.blackHoles[0].TurnsLeft)
	}
	cleared := TickBlackHoles(p, ov, Black)
	if len(cleared) != 2 {
		t.Fatalf("expected both squares' occupants destroyed on detonation, got %v", cleared)
	}
	if !p.PieceAt(sq1).IsNone() || !p.PieceAt(sq2).IsNone() {
		t.Fatal("expected both blackhole squares cleared")
	}
	if len(ov.blackHoles) != 0 {
		t.Fatal("expected the spent zone to be dropped")
	}
}

// -- Hash ------------------------------------------------------------------

func TestOverlayHashChangesWithEveryFieldAndIsStable(t *testing.T) {
	base := NewCardOverlay()
	if base.Hash() != base.Hash() {
		t.Fatal("Hash should be stable across repeated calls with no mutation")
	}
	baseHash := base.Hash()

	cases := map[string]*CardOverlay{}
	frozen := NewCardOverlay()
	frozen.SetFrozen(NewSquare(0, 0), true)
	cases["Frozen"] = frozen

	shielded := NewCardOverlay()
	shielded.SetShielded(NewSquare(0, 0), 5)
	cases["Shielded"] = shielded

	fused := NewCardOverlay()
	fused.SetFused(NewSquare(0, 0), Knight)
	cases["FusedWith"] = fused

	fortress := NewCardOverlay()
	fortress.SetFortress(White, NewSquare(2, 2), 2)
	cases["Fortress"] = fortress

	lava := NewCardOverlay()
	lava.AddLava(NewSquare(3, 3), 2)
	cases["Lava"] = lava

	bomb := NewCardOverlay()
	bomb.AddBomb(NewSquare(3, 3), White, 2)
	cases["Bomb"] = bomb

	hole := NewCardOverlay()
	hole.AddBlackHole(NewSquare(1, 1), NewSquare(6, 6), Black, 2)
	cases["BlackHole"] = hole

	for name, ov := range cases {
		if ov.Hash() == baseHash {
			t.Errorf("expected %s to change the hash relative to an empty overlay", name)
		}
	}
}

// TestMultisetOverlayFieldsDontCancelOnDuplicates locks in the reason
// Lava/Bomb/BlackHole use additive (not XOR) combination for their zone
// lists: internal/match structurally allows duplicate entries (a piece can
// be double-bombed -- the unabomber case never checks for an existing bomb
// on the target, match_cards.go:1006-1023), and plain XOR-per-entry would
// let two byte-for-byte-identical entries cancel back to zero contribution.
func TestMultisetOverlayFieldsDontCancelOnDuplicates(t *testing.T) {
	empty := NewCardOverlay()

	oneBomb := NewCardOverlay()
	oneBomb.AddBomb(NewSquare(2, 2), White, 2)

	twoIdenticalBombs := NewCardOverlay()
	twoIdenticalBombs.AddBomb(NewSquare(2, 2), White, 2)
	twoIdenticalBombs.AddBomb(NewSquare(2, 2), White, 2)

	if twoIdenticalBombs.Hash() == empty.Hash() {
		t.Fatal("two identical bomb timers must not hash the same as zero timers (XOR-cancellation bug)")
	}
	if twoIdenticalBombs.Hash() == oneBomb.Hash() {
		t.Fatal("two identical bomb timers should not hash the same as exactly one")
	}
}

// -- MakeMoveWithOverlay -------------------------------------------------

func TestMakeMoveWithOverlayTransfersFlagsAndClearsCapturedSquare(t *testing.T) {
	p := NewStartingPosition()
	from, to := NewSquare(4, 1), NewSquare(4, 3) // e2-e4
	ov := NewCardOverlay()
	ov.SetShielded(from, 0)
	ov.SetFused(from, Knight)

	MakeMoveWithOverlay(p, ov, Move{From: from, To: to, Flag: DoublePawnPush})

	if ov.IsShielded(from) || ov.FusedWith(from) != NoPieceType {
		t.Fatal("expected the vacated square's flags to be cleared")
	}
	if !ov.IsShielded(to) || ov.FusedWith(to) != Knight {
		t.Fatal("expected the mover's flags to travel to the destination")
	}
}

func TestMakeMoveWithOverlayClearsFlagsOnPromotion(t *testing.T) {
	p := NewEmptyPosition()
	from := NewSquare(0, 6) // a7
	to := NewSquare(0, 7)   // a8
	p.SetPiece(from, Piece{Type: Pawn, Color: White})
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black})
	ov := NewCardOverlay()
	ov.SetShielded(from, 0)

	MakeMoveWithOverlay(p, ov, Move{From: from, To: to, Promotion: Queen})

	if ov.IsShielded(to) {
		t.Fatal("a freshly-promoted piece must not inherit the pawn's shield")
	}
	if ov.IsShielded(from) {
		t.Fatal("expected the vacated pawn square's flags cleared too")
	}
}

func TestMakeMoveWithOverlayMovesRookFlagsOnCastling(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White}) // e1
	rookFrom := NewSquare(7, 0)                                  // h1
	p.SetPiece(rookFrom, Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black}) // e8
	p.castling = CastleWhiteKingside
	ov := NewCardOverlay()
	ov.SetFused(rookFrom, Bishop) // arbitrary marker, just to track movement

	MakeMoveWithOverlay(p, ov, Move{From: NewSquare(4, 0), To: NewSquare(6, 0), Flag: CastleKingside})

	rookTo := NewSquare(5, 0)
	if ov.FusedWith(rookFrom) != NoPieceType {
		t.Fatal("expected the rook's old square cleared")
	}
	if ov.FusedWith(rookTo) != Bishop {
		t.Fatal("expected the rook's flag to travel with it to its new square")
	}
}
