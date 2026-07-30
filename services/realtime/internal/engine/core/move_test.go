package core

import "testing"

// snapshot copies every field make/unmake can touch, for exact
// before/after comparison. Position's fields are all arrays/value types
// (no pointers or slices), so a plain struct copy is a real, independent
// snapshot -- not an alias that would trivially "match" itself.
func snapshot(p *Position) Position { return *p }

// assertRestored fails the test with a detailed diff if after != before.
func assertRestored(t *testing.T, before, after Position) {
	t.Helper()
	if before == after {
		return
	}
	if before.pieces != after.pieces {
		t.Errorf("piece bitboards differ after unmake:\n before=%+v\n after =%+v", before.pieces, after.pieces)
	}
	if before.occupied != after.occupied || before.occupiedAll != after.occupiedAll {
		t.Errorf("occupancy differs after unmake: before=%v/%v after=%v/%v",
			before.occupied, before.occupiedAll, after.occupied, after.occupiedAll)
	}
	if before.sideToMove != after.sideToMove {
		t.Errorf("sideToMove differs: before=%v after=%v", before.sideToMove, after.sideToMove)
	}
	if before.castling != after.castling {
		t.Errorf("castling rights differ: before=%#b after=%#b", before.castling, after.castling)
	}
	if before.enPassant != after.enPassant {
		t.Errorf("enPassant differs: before=%v after=%v", before.enPassant, after.enPassant)
	}
	if before.halfMoveClock != after.halfMoveClock {
		t.Errorf("halfMoveClock differs: before=%d after=%d", before.halfMoveClock, after.halfMoveClock)
	}
	if before.fullMoveNum != after.fullMoveNum {
		t.Errorf("fullMoveNum differs: before=%d after=%d", before.fullMoveNum, after.fullMoveNum)
	}
}

func TestMakeUnmakeQuietPawnPush(t *testing.T) {
	p := NewStartingPosition()
	before := snapshot(p)

	u := p.MakeMove(Move{From: NewSquare(4, 1), To: NewSquare(4, 3), Flag: DoublePawnPush})

	if p.PieceAt(NewSquare(4, 1)) != NoPiece {
		t.Error("source square e2 should be empty after the push")
	}
	if got := p.PieceAt(NewSquare(4, 3)); got.Type != Pawn || got.Color != White {
		t.Errorf("e4 should hold a white pawn, got %+v", got)
	}
	if p.sideToMove != Black {
		t.Error("side to move should flip to Black")
	}
	if p.enPassant != NewSquare(4, 2) {
		t.Errorf("en passant square should be e3, got %v", p.enPassant)
	}
	if p.halfMoveClock != 0 {
		t.Errorf("halfmove clock should reset on a pawn move, got %d", p.halfMoveClock)
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
}

func TestMakeUnmakeCapture(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 3), Piece{Type: Queen, Color: White})
	p.SetPiece(NewSquare(4, 6), Piece{Type: Pawn, Color: Black})
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	before := snapshot(p)

	u := p.MakeMove(Move{From: NewSquare(4, 3), To: NewSquare(4, 6)})

	if got := p.PieceAt(NewSquare(4, 6)); got.Type != Queen || got.Color != White {
		t.Errorf("expected white queen on e7 after capture, got %+v", got)
	}
	if p.halfMoveClock != 0 {
		t.Error("halfmove clock should reset on a capture")
	}
	if u.captured != Pawn {
		t.Errorf("undo should record a captured pawn, got %v", u.captured)
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
	if got := p.PieceAt(NewSquare(4, 6)); got.Type != Pawn || got.Color != Black {
		t.Errorf("expected the captured black pawn restored on e7, got %+v", got)
	}
}

func TestMakeUnmakeEnPassant(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 4), Piece{Type: Pawn, Color: White})  // e5
	p.SetPiece(NewSquare(3, 4), Piece{Type: Pawn, Color: Black})  // d5, just double-pushed
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.enPassant = NewSquare(3, 5) // d6
	before := snapshot(p)

	u := p.MakeMove(Move{From: NewSquare(4, 4), To: NewSquare(3, 5), Flag: EnPassantCapture})

	if got := p.PieceAt(NewSquare(3, 5)); got.Type != Pawn || got.Color != White {
		t.Errorf("expected white pawn on d6, got %+v", got)
	}
	if got := p.PieceAt(NewSquare(3, 4)); !got.IsNone() {
		t.Errorf("expected the captured black pawn's square (d5) to be empty, got %+v", got)
	}
	if p.occupiedAll.PopCount() != 3 {
		t.Errorf("expected exactly 3 pieces left on the board, got %d", p.occupiedAll.PopCount())
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
	if got := p.PieceAt(NewSquare(3, 4)); got.Type != Pawn || got.Color != Black {
		t.Errorf("expected the captured black pawn restored on d5, got %+v", got)
	}
}

func TestMakeUnmakeCastlingKingside(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.castling = CastleWhiteKingside | CastleWhiteQueenside
	before := snapshot(p)

	u := p.MakeMove(Move{From: NewSquare(4, 0), To: NewSquare(6, 0), Flag: CastleKingside})

	if got := p.PieceAt(NewSquare(6, 0)); got.Type != King || got.Color != White {
		t.Errorf("expected white king on g1, got %+v", got)
	}
	if got := p.PieceAt(NewSquare(5, 0)); got.Type != Rook || got.Color != White {
		t.Errorf("expected white rook on f1, got %+v", got)
	}
	if !p.PieceAt(NewSquare(4, 0)).IsNone() || !p.PieceAt(NewSquare(7, 0)).IsNone() {
		t.Error("e1 and h1 should both be empty after castling")
	}
	if p.castling&(CastleWhiteKingside|CastleWhiteQueenside) != 0 {
		t.Error("both white castling rights should be cleared once the king has castled")
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
}

func TestMakeUnmakeCastlingQueenside(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black})
	p.SetPiece(NewSquare(0, 7), Piece{Type: Rook, Color: Black})
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.castling = CastleBlackKingside | CastleBlackQueenside
	before := snapshot(p)

	u := p.MakeMove(Move{From: NewSquare(4, 7), To: NewSquare(2, 7), Flag: CastleQueenside})

	if got := p.PieceAt(NewSquare(2, 7)); got.Type != King || got.Color != Black {
		t.Errorf("expected black king on c8, got %+v", got)
	}
	if got := p.PieceAt(NewSquare(3, 7)); got.Type != Rook || got.Color != Black {
		t.Errorf("expected black rook on d8, got %+v", got)
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
}

func TestMakeUnmakePromotionWithCapture(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 6), Piece{Type: Pawn, Color: White})  // e7
	p.SetPiece(NewSquare(3, 7), Piece{Type: Rook, Color: Black})  // d8
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	before := snapshot(p)

	u := p.MakeMove(Move{From: NewSquare(4, 6), To: NewSquare(3, 7), Promotion: Queen})

	if got := p.PieceAt(NewSquare(3, 7)); got.Type != Queen || got.Color != White {
		t.Errorf("expected a white queen on d8 after exd8=Q, got %+v", got)
	}
	if got := p.PieceBitboard(Pawn, White); got.Any() {
		t.Error("the promoted pawn should no longer exist as a pawn")
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
	if got := p.PieceAt(NewSquare(4, 6)); got.Type != Pawn || got.Color != White {
		t.Errorf("expected the pawn restored (not the queen) on e7, got %+v", got)
	}
	if got := p.PieceAt(NewSquare(3, 7)); got.Type != Rook || got.Color != Black {
		t.Errorf("expected the captured black rook restored on d8, got %+v", got)
	}
}

func TestCastlingRightsRevokedByRookCapture(t *testing.T) {
	// A rook captured on its OWN home square must revoke that side's
	// castling right even though the king never moved -- a classic bug
	// class if the "which rights does this move affect" logic only checks
	// the moving piece's identity/source and ignores the destination.
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.SetPiece(NewSquare(7, 6), Piece{Type: Bishop, Color: Black})
	p.castling = CastleWhiteKingside | CastleWhiteQueenside

	before := snapshot(p)
	u := p.MakeMove(Move{From: NewSquare(7, 6), To: NewSquare(7, 0)}) // Bxh1

	if p.HasCastleRight(CastleWhiteKingside) {
		t.Error("capturing the h1 rook must revoke white's kingside castling right")
	}
	if !p.HasCastleRight(CastleWhiteQueenside) {
		t.Error("capturing the h1 rook must NOT revoke white's queenside right")
	}

	p.UnmakeMove(u)
	assertRestored(t, before, snapshot(p))
}

func TestHalfMoveClockIncrementsOnQuietNonPawnMove(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.halfMoveClock = 5

	u := p.MakeMove(Move{From: NewSquare(0, 0), To: NewSquare(1, 0)})
	if p.halfMoveClock != 6 {
		t.Errorf("expected halfmove clock to increment to 6, got %d", p.halfMoveClock)
	}
	p.UnmakeMove(u)
	if p.halfMoveClock != 5 {
		t.Errorf("expected halfmove clock restored to 5, got %d", p.halfMoveClock)
	}
}

func TestFullMoveNumberIncrementsOnlyAfterBlack(t *testing.T) {
	p := NewStartingPosition() // white to move, fullMoveNum = 1
	u1 := p.MakeMove(Move{From: NewSquare(4, 1), To: NewSquare(4, 3), Flag: DoublePawnPush})
	if p.fullMoveNum != 1 {
		t.Errorf("fullmove number should still be 1 after White's move, got %d", p.fullMoveNum)
	}
	u2 := p.MakeMove(Move{From: NewSquare(4, 6), To: NewSquare(4, 4), Flag: DoublePawnPush})
	if p.fullMoveNum != 2 {
		t.Errorf("fullmove number should be 2 after Black's move, got %d", p.fullMoveNum)
	}
	p.UnmakeMove(u2)
	if p.fullMoveNum != 1 {
		t.Errorf("expected fullmove number restored to 1, got %d", p.fullMoveNum)
	}
	p.UnmakeMove(u1)
	if p.fullMoveNum != 1 {
		t.Errorf("expected fullmove number to remain 1 after undoing White's first move, got %d", p.fullMoveNum)
	}
}

func TestEnPassantSquareClearsAfterAnyNonDoublePushMove(t *testing.T) {
	p := NewStartingPosition()
	u1 := p.MakeMove(Move{From: NewSquare(4, 1), To: NewSquare(4, 3), Flag: DoublePawnPush})
	if p.enPassant == NoSquare {
		t.Fatal("expected an en passant square after a double push")
	}
	u2 := p.MakeMove(Move{From: NewSquare(1, 7), To: NewSquare(2, 5)}) // an unrelated knight move
	if p.enPassant != NoSquare {
		t.Errorf("en passant square must clear after any move that isn't itself a double push, got %v", p.enPassant)
	}
	p.UnmakeMove(u2)
	if p.enPassant == NoSquare {
		t.Error("unmaking the knight move should restore the prior en passant square")
	}
	p.UnmakeMove(u1)
}

// pieceBitboardsAreConsistent reports whether every piece-type bitboard
// is pairwise disjoint and their union matches Occupied/OccupiedAll --
// the invariant TestMakeMoveIgnoresPawnOnlyFlagsForANonPawnMover checks,
// since a two-piece-types-claim-the-same-square corruption wouldn't
// necessarily show up in assertRestored's before/after diff if the
// corruption happens to leave the FINAL restored state looking correct
// while the intermediate (post-MakeMove, pre-Unmake) state is broken.
func pieceBitboardsAreConsistent(p *Position) bool {
	var union, whiteUnion, blackUnion Bitboard
	for _, pt := range []PieceType{Pawn, Knight, Bishop, Rook, Queen, King} {
		for _, c := range []Color{White, Black} {
			bb := p.PieceBitboard(pt, c)
			if bb&union != 0 {
				return false
			}
			union |= bb
			if c == White {
				whiteUnion |= bb
			} else {
				blackUnion |= bb
			}
		}
	}
	return union == p.OccupiedAll() && whiteUnion == p.Occupied(White) && blackUnion == p.Occupied(Black)
}

// TestMakeMoveIgnoresPawnOnlyFlagsForANonPawnMover is a permanent
// regression test for a real corruption bug: engine/core/overlays_movegen.go's
// generateFusionMoves lets a piece fused with Pawn (CardOverlay.fusedWith)
// generate moves via the full pawn move pattern -- including
// DoublePawnPush, EnPassantCapture, and promotion -- matching
// internal/match's own legalMovesWithFusion, which generates candidates
// against a board with the piece temporarily RELABELED as its fused type.
// But the reference gates every pawn-specific CONSEQUENCE of applying
// such a move (chess.go's `moving.Type == "pawn"` checks in movePiece)
// on the piece's REAL type, never on how the candidate was generated --
// and this package's MakeMove/UnmakeMove used to trust
// m.Flag/m.IsPromotion() unconditionally instead. Applying a
// promotion-flagged move for a REAL Knight (e.g. fused with Pawn,
// "promoting" via the fabricated pattern on reaching the back rank)
// moved the Knight to the destination via movePiece, then ALSO set the
// promoted piece type there without ever clearing the Knight's own bit --
// two piece-type bitboards claiming the same square. Found via a real
// gauntlet run (Task 10) crashing with exactly this shape of corruption
// several plies later, root-caused via an isolated
// actions.ApplyCardAction consistency test (actions/fusion_diag_test.go)
// that first showed applyFusion itself was clean, narrowing it down to
// this file. Fixed via undo.realPawnMove, decided once from the mover's
// ACTUAL type at MakeMove time and consulted symmetrically by UnmakeMove.
func TestMakeMoveIgnoresPawnOnlyFlagsForANonPawnMover(t *testing.T) {
	t.Run("promotion-flagged move for a real Knight does not corrupt bitboards", func(t *testing.T) {
		p := NewEmptyPosition()
		p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
		p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black})
		knightSq := NewSquare(2, 6) // c7, one step from a fabricated "promotion" on rank 8
		p.SetPiece(knightSq, Piece{Type: Knight, Color: White})
		before := snapshot(p)
		if !pieceBitboardsAreConsistent(p) {
			t.Fatal("test setup itself is inconsistent")
		}

		// A move no REAL knight move generator would ever produce (a
		// diagonal one-step landing on the promotion rank with a
		// Promotion field set) -- exactly the shape generateFusionMoves
		// can fabricate for a Knight fused with Pawn.
		fake := Move{From: knightSq, To: NewSquare(3, 7), Promotion: Queen, Flag: Quiet}
		u := p.MakeMove(fake)
		if !pieceBitboardsAreConsistent(p) {
			t.Fatalf("bitboards inconsistent after MakeMove: FEN=%q", p.ToFEN())
		}
		if got := p.PieceAt(fake.To); got.Type != Knight || got.Color != White {
			t.Fatalf("expected the REAL knight (not a queen) to have moved to %v, got %+v", fake.To, got)
		}
		if !p.PieceAt(fake.From).IsNone() {
			t.Fatalf("expected %v to be empty after the knight moved away", fake.From)
		}

		p.UnmakeMove(u)
		assertRestored(t, before, snapshot(p))
	})

	t.Run("en-passant-flagged move for a real Bishop is a plain relocation", func(t *testing.T) {
		p := NewEmptyPosition()
		p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
		p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black})
		bishopSq := NewSquare(3, 3) // d4
		p.SetPiece(bishopSq, Piece{Type: Bishop, Color: White})
		// A real pawn sitting where EnPassantCapture's capture-square math
		// would look -- if the bug were present, this pawn would be
		// wrongly removed even though the REAL mover isn't a pawn.
		phantomVictimSq := NewSquare(4, 3) // e4
		p.SetPiece(phantomVictimSq, Piece{Type: Pawn, Color: Black})
		before := snapshot(p)

		fake := Move{From: bishopSq, To: NewSquare(4, 2), Flag: EnPassantCapture} // d4 -> e3
		u := p.MakeMove(fake)
		if !pieceBitboardsAreConsistent(p) {
			t.Fatalf("bitboards inconsistent after MakeMove: FEN=%q", p.ToFEN())
		}
		if got := p.PieceAt(phantomVictimSq); got.Type != Pawn || got.Color != Black {
			t.Fatalf("expected the real black pawn on e4 to be untouched (mover wasn't really a pawn), got %+v", got)
		}
		if got := p.PieceAt(fake.To); got.Type != Bishop || got.Color != White {
			t.Fatalf("expected the bishop to have relocated to %v, got %+v", fake.To, got)
		}

		p.UnmakeMove(u)
		assertRestored(t, before, snapshot(p))
	})

	t.Run("double-pawn-push-flagged move for a real Rook does not open an en passant square", func(t *testing.T) {
		p := NewEmptyPosition()
		p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
		p.SetPiece(NewSquare(4, 7), Piece{Type: King, Color: Black})
		rookSq := NewSquare(0, 1) // a2
		p.SetPiece(rookSq, Piece{Type: Rook, Color: White})

		fake := Move{From: rookSq, To: NewSquare(0, 3), Flag: DoublePawnPush} // a2 -> a4
		u := p.MakeMove(fake)
		if p.EnPassant() != NoSquare {
			t.Fatalf("expected no en-passant square to open for a non-pawn's move, got %v", p.EnPassant())
		}
		p.UnmakeMove(u)
	})
}

// TestMakeUnmakeSequenceRestoresStartingPosition plays a short, ordinary
// sequence of moves (including a capture) and unmakes them in reverse order,
// checking that the ENTIRE chain returns exactly to the starting position --
// the property make/unmake actually needs to have under real search
// recursion, not just single move/unmove round trips.
func TestMakeUnmakeSequenceRestoresStartingPosition(t *testing.T) {
	p := NewStartingPosition()
	before := snapshot(p)

	moves := []Move{
		{From: NewSquare(4, 1), To: NewSquare(4, 3), Flag: DoublePawnPush}, // e4
		{From: NewSquare(4, 6), To: NewSquare(4, 4), Flag: DoublePawnPush}, // e5
		{From: NewSquare(6, 0), To: NewSquare(5, 2)},                      // Nf3
		{From: NewSquare(1, 7), To: NewSquare(2, 5)},                      // Nc6
		{From: NewSquare(5, 0), To: NewSquare(1, 4)},                      // Bb5
	}

	var undos []undo
	for _, m := range moves {
		undos = append(undos, p.MakeMove(m))
	}
	for i := len(undos) - 1; i >= 0; i-- {
		p.UnmakeMove(undos[i])
	}

	assertRestored(t, before, snapshot(p))
}
