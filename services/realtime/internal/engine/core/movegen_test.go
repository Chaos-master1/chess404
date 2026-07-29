package core

import "testing"

func TestStartingPositionHasExactly20LegalMoves(t *testing.T) {
	p := NewStartingPosition()
	moves := GenerateLegalMoves(p)
	// 8 pawns x 2 (single + double push) + 2 knights x 2 (each knight has
	// exactly 2 legal jumps from the back rank) = 16 + 4 = 20. This is one of
	// the most well-known reference numbers in chess programming.
	if got := len(moves); got != 20 {
		t.Fatalf("starting position: %d legal moves, want 20", got)
	}
}

func TestPromotionGeneratesAllFourPieceChoices(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 6), Piece{Type: Pawn, Color: White}) // e7, one step from promoting
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})

	moves := GenerateLegalMoves(p)
	promos := map[PieceType]bool{}
	for _, m := range moves {
		if m.From == NewSquare(4, 6) && m.To == NewSquare(4, 7) {
			promos[m.Promotion] = true
		}
	}
	for _, want := range []PieceType{Knight, Bishop, Rook, Queen} {
		if !promos[want] {
			t.Errorf("expected a promotion choice to %v, not generated", want)
		}
	}
	if len(promos) != 4 {
		t.Errorf("expected exactly 4 promotion choices, got %d: %v", len(promos), promos)
	}
}

func TestEnPassantIsGeneratedWhenAvailable(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 4), Piece{Type: Pawn, Color: White}) // e5
	p.SetPiece(NewSquare(3, 4), Piece{Type: Pawn, Color: Black}) // d5, just double-pushed
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.enPassant = NewSquare(3, 5) // d6

	moves := GenerateLegalMoves(p)
	found := false
	for _, m := range moves {
		if m.Flag == EnPassantCapture {
			found = true
			if m.From != NewSquare(4, 4) || m.To != NewSquare(3, 5) {
				t.Errorf("unexpected en passant move %+v", m)
			}
		}
	}
	if !found {
		t.Error("expected an en passant capture to be generated")
	}
}

func TestEnPassantNotGeneratedWithoutTheRightPawnAdjacent(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 4), Piece{Type: Pawn, Color: White}) // e5, NOT adjacent to the ep file
	p.SetPiece(NewSquare(0, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.enPassant = NewSquare(1, 5) // b6 -- unrelated to e5

	moves := GenerateLegalMoves(p)
	for _, m := range moves {
		if m.Flag == EnPassantCapture {
			t.Errorf("did not expect an en passant move here, got %+v", m)
		}
	}
}

func TestPinnedPieceCannotExposeKingToCheck(t *testing.T) {
	// White king on e1, white bishop pinned on e3 by a black rook on e8.
	// Pseudo-legal generation will offer the bishop plenty of diagonal moves;
	// none of them may be legal, since every one exposes the king along the
	// e-file.
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(4, 2), Piece{Type: Bishop, Color: White})
	p.SetPiece(NewSquare(4, 7), Piece{Type: Rook, Color: Black})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})

	moves := GenerateLegalMoves(p)
	for _, m := range moves {
		if m.From == NewSquare(4, 2) {
			t.Errorf("pinned bishop must not have any legal moves off the e-file, got %+v", m)
		}
	}

	// Sanity: the same bishop, moved off the pin line entirely (nothing
	// between it and the rook), regains its normal mobility.
	pseudo := GenerateMoves(p, nil)
	bishopPseudoMoves := 0
	for _, m := range pseudo {
		if m.From == NewSquare(4, 2) {
			bishopPseudoMoves++
		}
	}
	if bishopPseudoMoves == 0 {
		t.Fatal("test setup error: expected the bishop to have pseudo-legal moves before legality filtering")
	}
}

func TestCheckEvasionOnlyGeneratesMovesThatResolveCheck(t *testing.T) {
	// White king on e1, in check from a black KNIGHT on d3 -- a knight check
	// can never be blocked (there is no square "between" a knight and its
	// target), only captured or evaded by moving the king. A white knight on
	// a1, unrelated to the check, has ordinary moves available (b3, c2) but
	// none of them capture the checking knight or move the king, so none of
	// them should survive legal filtering. This is a stricter, unambiguous
	// version of the original test, which mistakenly used a rook check a
	// knight move could legally block along the file.
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(0, 0), Piece{Type: Knight, Color: White})
	p.SetPiece(NewSquare(3, 2), Piece{Type: Knight, Color: Black})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})

	if !p.InCheck() {
		t.Fatal("test setup error: white should be in check from the knight on d3")
	}

	moves := GenerateLegalMoves(p)
	for _, m := range moves {
		if m.From == NewSquare(0, 0) {
			t.Errorf("the knight on a1 can neither capture nor block this knight check; its move %+v should have been filtered", m)
		}
	}
	if len(moves) == 0 {
		t.Fatal("expected at least one legal move (a king move away from the checking knight)")
	}
	for _, m := range moves {
		if m.From != NewSquare(4, 0) {
			t.Errorf("expected every legal move to be a king move in this position, got %+v", m)
		}
	}
}

func TestCastlingGeneratedWhenClearAndUnattacked(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(0, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.castling = CastleWhiteKingside | CastleWhiteQueenside

	moves := GenerateLegalMoves(p)
	hasKingside, hasQueenside := false, false
	for _, m := range moves {
		if m.Flag == CastleKingside {
			hasKingside = true
		}
		if m.Flag == CastleQueenside {
			hasQueenside = true
		}
	}
	if !hasKingside {
		t.Error("expected kingside castling to be available")
	}
	if !hasQueenside {
		t.Error("expected queenside castling to be available")
	}
}

func TestCastlingBlockedByAttackedTransitSquare(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	// A black rook on f8 attacks f1 -- the square the king must pass
	// through to castle kingside.
	p.SetPiece(NewSquare(5, 7), Piece{Type: Rook, Color: Black})
	p.castling = CastleWhiteKingside

	moves := GenerateLegalMoves(p)
	for _, m := range moves {
		if m.Flag == CastleKingside {
			t.Error("castling through an attacked square must not be legal")
		}
	}
}

func TestCastlingBlockedByOccupiedSquare(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(5, 0), Piece{Type: Bishop, Color: White}) // f1 occupied
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.castling = CastleWhiteKingside

	moves := GenerateLegalMoves(p)
	for _, m := range moves {
		if m.Flag == CastleKingside {
			t.Error("castling through an occupied square must not be legal")
		}
	}
}

func TestNoLegalMovesIsCheckmateOrStalemateDependingOnCheck(t *testing.T) {
	// Fool's mate final position: White has just been mated. No legal moves,
	// and White IS in check -- checkmate, not stalemate.
	p, err := ParseFEN("rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3")
	if err != nil {
		t.Fatalf("failed to parse FEN: %v", err)
	}
	moves := GenerateLegalMoves(p)
	if len(moves) != 0 {
		t.Fatalf("expected no legal moves in the mated position, got %d: %v", len(moves), moves)
	}
	if !p.InCheck() {
		t.Error("expected the position to be check (this is checkmate, not stalemate)")
	}
}
