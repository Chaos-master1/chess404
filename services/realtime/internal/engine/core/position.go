package core

// Position is the bitboard board representation: 12 piece bitboards plus the
// side-to-move, castling, en-passant, and clock state needed to make and
// unmake moves correctly. This is the direct replacement for the previous
// search substrate's per-node cloneMatchState/cloneBoard -- a full deep copy
// of a 46-field wire-format struct, board included, at every single node.
// Position instead supports MakeMove/UnmakeMove: apply a move in place,
// remember just enough to undo it, and restore in O(1) instead of
// reallocating the whole position.
//
// Card overlay state (frozen/shielded/lava/fortress/... bit planes) is
// deliberately NOT on this struct -- see overlays.go, added once plain chess
// is perft-verified. Keeping this struct chess-only until then means perft
// correctness can be checked against the standard, well-known reference
// values completely independently of anything Chess404-specific.
type Position struct {
	// pieces[Piece{type,color}.Index()] is that piece's bitboard.
	pieces [12]Bitboard
	// occupied[White]/occupied[Black] are the union of that color's 6 piece
	// bitboards; occupiedAll is the union of both. All three are redundant
	// with pieces but kept incrementally updated because they're read on
	// nearly every attack/legality query -- recomputing them by OR-ing 6 or
	// 12 bitboards on every call would undo a good chunk of what bitboards
	// are for.
	occupied    [2]Bitboard
	occupiedAll Bitboard

	sideToMove Color

	// castling is a 4-bit field: bit 0 = white kingside, 1 = white queenside,
	// 2 = black kingside, 3 = black queenside. See the CastleXxx constants.
	castling uint8

	// enPassant is the square a pawn can capture TO this move (i.e. one
	// square behind a pawn that just double-pushed), or NoSquare if the last
	// move wasn't a double push. Matches the standard FEN en-passant-square
	// convention, not the moved pawn's own square.
	enPassant Square

	halfMoveClock int
	fullMoveNum   int

	// hash is the position's Zobrist key, maintained incrementally by
	// SetPiece/removePiece/movePiece (the piece-placement contribution) and
	// MakeMove/UnmakeMove (the side/castling/en-passant contribution). See
	// zobrist.go.
	hash uint64
}

const (
	CastleWhiteKingside uint8 = 1 << iota
	CastleWhiteQueenside
	CastleBlackKingside
	CastleBlackQueenside
)

// NewEmptyPosition returns a Position with no pieces, White to move, no
// castling rights, and no en-passant square -- the zero state callers build
// a real position up from (tests, FEN parsing, or converting from a
// contracts.MatchState).
func NewEmptyPosition() *Position {
	return &Position{
		sideToMove: White,
		enPassant:  NoSquare,
	}
}

// NewStartingPosition returns the standard chess starting position.
func NewStartingPosition() *Position {
	p := NewEmptyPosition()
	back := [8]PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for file := 0; file < 8; file++ {
		p.SetPiece(NewSquare(file, 0), Piece{Type: back[file], Color: White})
		p.SetPiece(NewSquare(file, 1), Piece{Type: Pawn, Color: White})
		p.SetPiece(NewSquare(file, 6), Piece{Type: Pawn, Color: Black})
		p.SetPiece(NewSquare(file, 7), Piece{Type: back[file], Color: Black})
	}
	p.castling = CastleWhiteKingside | CastleWhiteQueenside | CastleBlackKingside | CastleBlackQueenside
	p.fullMoveNum = 1
	// SetPiece above kept the piece-placement part of the hash correct
	// incrementally, but castling/side/en-passant were just set directly
	// above and in NewEmptyPosition, bypassing the hash-aware helpers (there
	// would be little point making one-time setup code incremental) -- a
	// full recompute is the simple, obviously-correct way to finish
	// initializing hash for any construction path, done once here and again
	// at the end of ParseFEN, never per-move.
	p.hash = computeHash(p)
	return p
}

func (p *Position) SideToMove() Color   { return p.sideToMove }
func (p *Position) EnPassant() Square   { return p.enPassant }
func (p *Position) HalfMoveClock() int  { return p.halfMoveClock }
func (p *Position) FullMoveNumber() int { return p.fullMoveNum }
func (p *Position) HasCastleRight(right uint8) bool { return p.castling&right != 0 }
func (p *Position) Occupied(c Color) Bitboard { return p.occupied[c] }
func (p *Position) OccupiedAll() Bitboard     { return p.occupiedAll }
func (p *Position) PieceBitboard(pt PieceType, c Color) Bitboard {
	return p.pieces[(Piece{Type: pt, Color: c}).Index()]
}

// PieceAt returns the piece on sq, or NoPiece if it's empty. This does a
// linear scan of the 12 piece bitboards -- fine for the relatively rare
// callers that need "what's on this specific square" (move generation reads
// bitboards directly and never needs this); it is NOT used in any hot movegen
// loop.
func (p *Position) PieceAt(sq Square) Piece {
	if !p.occupiedAll.Has(sq) {
		return NoPiece
	}
	for idx := 0; idx < 12; idx++ {
		if p.pieces[idx].Has(sq) {
			// +1 inverts Piece.Index()'s -1 (see its comment): idx is in the
			// 0-5 packed slot range, PieceType's real values start at 1.
			return Piece{Type: PieceType(idx%6 + 1), Color: Color(idx / 6)}
		}
	}
	return NoPiece // unreachable if occupiedAll is kept consistent
}

// SetPiece places piece on sq. sq must currently be empty -- this is a raw
// board-setup primitive (used by NewStartingPosition and FEN parsing), not a
// move; it does not touch castling/en-passant/clocks and does not check
// legality. Maintains Hash() incrementally, same as the move-application
// primitives below -- setup calls simply XOR each piece's key in one at a
// time, which is exactly equivalent to a bulk recompute afterward (XOR is
// order-independent), so board setup and move application share one
// hash-correctness story instead of two.
func (p *Position) SetPiece(sq Square, piece Piece) {
	p.pieces[piece.Index()] = p.pieces[piece.Index()].Set(sq)
	p.occupied[piece.Color] = p.occupied[piece.Color].Set(sq)
	p.occupiedAll = p.occupiedAll.Set(sq)
	p.hash ^= pieceZobristKey(piece, sq)
}

// RemovePiece clears sq's occupant, if any (a no-op on an already-empty
// square) -- the general "delete a piece outright" primitive several card
// mechanics need (Phase 2's engine/actions: fusion consumes its first
// piece; sniper/badsniper/parasite/sacrifice remove pieces directly), none
// of which are a "move" in this package's sense. Unlike SetPiece (which
// requires an empty destination), this is specifically for removal.
func (p *Position) RemovePiece(sq Square) {
	piece := p.PieceAt(sq)
	if piece.IsNone() {
		return
	}
	p.removePiece(sq, piece)
}

// removePiece clears whatever piece (if any) sits on sq from all bitboards.
// Internal helper for make/unmake and RemovePiece; SetPiece is the public
// board-setup primitive for placing pieces on an empty board.
func (p *Position) removePiece(sq Square, piece Piece) {
	p.pieces[piece.Index()] = p.pieces[piece.Index()].Clear(sq)
	p.occupied[piece.Color] = p.occupied[piece.Color].Clear(sq)
	p.occupiedAll = p.occupiedAll.Clear(sq)
	p.hash ^= pieceZobristKey(piece, sq)
}

func (p *Position) movePiece(from, to Square, piece Piece) {
	mask := from.Bit() | to.Bit()
	p.pieces[piece.Index()] ^= mask
	p.occupied[piece.Color] ^= mask
	p.occupiedAll ^= mask
	p.hash ^= pieceZobristKey(piece, from) ^ pieceZobristKey(piece, to)
}
