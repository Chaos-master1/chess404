package core

// MoveFlag distinguishes move shapes that need special handling beyond "piece
// goes from A to B": en-passant capture removes a piece NOT on the
// destination square, castling also moves a rook, and a double pawn push
// opens up an en-passant square for the opponent's next move. Ordinary
// captures need no flag of their own -- MakeMove detects them by checking
// whether the destination is occupied.
type MoveFlag uint8

const (
	Quiet MoveFlag = iota
	EnPassantCapture
	CastleKingside
	CastleQueenside
	DoublePawnPush
)

// Move is intentionally a plain struct of small fixed-size fields (Square and
// PieceType are int8-based, see types.go) rather than the previous engine's
// Move (services/realtime/internal/engine/search.go), which carried a string
// Promotion field and was looked up in the transposition table, killer-move,
// and counter-move tables via fmt.Sprintf'd string keys
// (search.go:keyForSquare/counterMoveKey) -- string formatting and map
// allocation on every single node. Every field here is a value type; nothing
// in this package ever needs to allocate or format a Move to use it as a key
// (see zobrist.go for how positions, not moves, get hashed for the TT).
type Move struct {
	From, To  Square
	Promotion PieceType // NoPieceType unless this move promotes a pawn
	Flag      MoveFlag
}

// IsPromotion reports whether this move promotes a pawn.
func (m Move) IsPromotion() bool { return m.Promotion != NoPieceType }

// undo captures exactly what MakeMove changed beyond the piece placement
// itself (which UnmakeMove can reverse by re-examining the move), so
// UnmakeMove can restore the position in O(1) without keeping a full
// position snapshot per ply -- the entire point of make/unmake over the
// previous engine's per-node deep copy.
type undo struct {
	move             Move
	captured         PieceType // NoPieceType if the move was not a capture
	prevCastling     uint8
	prevEnPassant    Square
	prevHalfMoveClock int
}

// MakeMove applies move to the position in place and returns an undo token
// that UnmakeMove needs to reverse it. The caller is responsible for having
// generated a legal (or at least pseudo-legal, if checking for
// self-check afterward) move -- MakeMove does not validate legality, matching
// standard engine practice: legality filtering happens once during move
// generation, not redundantly on every apply.
func (p *Position) MakeMove(m Move) undo {
	mover := p.PieceAt(m.From)
	captured := NoPieceType
	if capPiece := p.PieceAt(m.To); !capPiece.IsNone() && m.Flag != CastleKingside && m.Flag != CastleQueenside {
		captured = capPiece.Type
	}

	u := undo{
		move:              m,
		captured:          captured,
		prevCastling:      p.castling,
		prevEnPassant:     p.enPassant,
		prevHalfMoveClock: p.halfMoveClock,
	}

	// Clock bookkeeping happens before mutating the board: both the "was
	// this a pawn move" and "was this a capture" checks need the
	// pre-move state.
	if mover.Type == Pawn || captured != NoPieceType {
		p.halfMoveClock = 0
	} else {
		p.halfMoveClock++
	}
	if p.sideToMove == Black {
		p.fullMoveNum++
	}

	switch m.Flag {
	case EnPassantCapture:
		capSq := NewSquare(m.To.File(), m.From.Rank())
		p.removePiece(capSq, Piece{Type: Pawn, Color: mover.Color.Opposite()})
		p.movePiece(m.From, m.To, mover)
		u.captured = Pawn // en passant is always a pawn capture; PieceAt(m.To) above found nothing, since the captured pawn isn't on m.To

	case CastleKingside, CastleQueenside:
		p.movePiece(m.From, m.To, mover)
		rookFrom, rookTo := castleRookSquares(mover.Color, m.Flag)
		p.movePiece(rookFrom, rookTo, Piece{Type: Rook, Color: mover.Color})

	default:
		if captured != NoPieceType {
			p.removePiece(m.To, Piece{Type: captured, Color: mover.Color.Opposite()})
		}
		p.movePiece(m.From, m.To, mover)
		if m.IsPromotion() {
			// The pawn already "moved" to m.To above; swap it for the
			// promoted piece there.
			p.removePiece(m.To, Piece{Type: Pawn, Color: mover.Color})
			p.SetPiece(m.To, Piece{Type: m.Promotion, Color: mover.Color})
		}
	}

	p.hash ^= enPassantZobristKey(p.enPassant) // remove the outgoing ep contribution
	p.enPassant = NoSquare
	if m.Flag == DoublePawnPush {
		p.enPassant = NewSquare(m.From.File(), (m.From.Rank()+m.To.Rank())/2)
	}
	p.hash ^= enPassantZobristKey(p.enPassant) // add the new one (both are 0/no-op when there's no ep square)

	oldCastling := p.castling
	p.castling &^= castlingRightsClearedBy(m.From) | castlingRightsClearedBy(m.To)
	p.hash ^= castlingHashDelta(oldCastling, p.castling)

	p.sideToMove = p.sideToMove.Opposite()
	p.hash ^= zobristSideKey
	return u
}

// castlingHashDelta returns the XOR of every castling-right key whose bit
// differs between old and new -- shared by MakeMove (rights only ever clear)
// and UnmakeMove (rights are restored via direct assignment, so a right can
// "change" in either direction there), so both sides of a make/unmake pair
// compute the castling contribution the exact same way.
func castlingHashDelta(oldRights, newRights uint8) uint64 {
	changed := oldRights ^ newRights
	var delta uint64
	for _, right := range [4]uint8{CastleWhiteKingside, CastleWhiteQueenside, CastleBlackKingside, CastleBlackQueenside} {
		if changed&right != 0 {
			delta ^= castleZobristKey(right)
		}
	}
	return delta
}

// UnmakeMove reverses exactly the change MakeMove made, given the undo token
// it returned. Must be called with the SAME move/token pair MakeMove
// produced, and only once, immediately after the corresponding MakeMove (the
// standard make/unmake contract -- this is not a general-purpose position
// stack).
func (p *Position) UnmakeMove(u undo) {
	p.sideToMove = p.sideToMove.Opposite()
	p.hash ^= zobristSideKey
	m := u.move
	mover := p.PieceAt(m.To) // after un-flipping side, m.To still holds the piece that just moved (or its promoted form)

	switch m.Flag {
	case EnPassantCapture:
		p.movePiece(m.To, m.From, mover)
		capSq := NewSquare(m.To.File(), m.From.Rank())
		p.SetPiece(capSq, Piece{Type: Pawn, Color: mover.Color.Opposite()})

	case CastleKingside, CastleQueenside:
		p.movePiece(m.To, m.From, mover)
		rookFrom, rookTo := castleRookSquares(mover.Color, m.Flag)
		p.movePiece(rookTo, rookFrom, Piece{Type: Rook, Color: mover.Color})

	default:
		if m.IsPromotion() {
			p.removePiece(m.To, Piece{Type: m.Promotion, Color: mover.Color})
			p.SetPiece(m.To, Piece{Type: Pawn, Color: mover.Color})
			mover = Piece{Type: Pawn, Color: mover.Color}
		}
		p.movePiece(m.To, m.From, mover)
		if u.captured != NoPieceType {
			p.SetPiece(m.To, Piece{Type: u.captured, Color: mover.Color.Opposite()})
		}
	}

	if p.sideToMove == Black {
		p.fullMoveNum--
	}
	p.hash ^= castlingHashDelta(p.castling, u.prevCastling)
	p.castling = u.prevCastling
	p.hash ^= enPassantZobristKey(p.enPassant) ^ enPassantZobristKey(u.prevEnPassant)
	p.enPassant = u.prevEnPassant
	p.halfMoveClock = u.prevHalfMoveClock
}

// castleRookSquares returns the rook's (from, to) squares for a castle of the
// given color/side. Only ever consulted for an actual CastleKingside/
// CastleQueenside move, so it doesn't need to validate the color/flag
// combination.
func castleRookSquares(c Color, flag MoveFlag) (from, to Square) {
	rank := 0
	if c == Black {
		rank = 7
	}
	if flag == CastleKingside {
		return NewSquare(7, rank), NewSquare(5, rank)
	}
	return NewSquare(0, rank), NewSquare(3, rank)
}

// castlingRightsClearedBy returns the castling-right bits that must be
// revoked because sq was just vacated or landed on -- covers both "the king
// or rook moved" (checked against m.From) and "a rook was captured on its
// home square" (checked against m.To) in one lookup, since both cases clear
// exactly the same bit for exactly the same reason: that corner's rook (or
// the king that castles with it) is no longer available.
func castlingRightsClearedBy(sq Square) uint8 {
	switch sq {
	case NewSquare(4, 0):
		return CastleWhiteKingside | CastleWhiteQueenside
	case NewSquare(7, 0):
		return CastleWhiteKingside
	case NewSquare(0, 0):
		return CastleWhiteQueenside
	case NewSquare(4, 7):
		return CastleBlackKingside | CastleBlackQueenside
	case NewSquare(7, 7):
		return CastleBlackKingside
	case NewSquare(0, 7):
		return CastleBlackQueenside
	default:
		return 0
	}
}
