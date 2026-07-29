package core

import "math/rand"

// Zobrist hashing, incrementally maintained. This directly replaces the
// previous engine's zobrist.go, whose Hash() recomputed the ENTIRE key from
// scratch at every node (a 64-square board scan plus a map allocation via
// sliceToSet(state.Moved) inside the loop) despite a doc comment on that same
// file claiming it was "XOR-ed incrementally so that apply/unapply is O(1)"
// -- it wasn't. Here it actually is: Position carries its own running hash,
// XORed in Position.Hash() as MakeMove/UnmakeMove change exactly the pieces
// of state that changed, nothing more.
//
// The other half of that same bug: the old hash covered board + side + ep +
// castling ONLY -- not hands, not card modifiers, not Frozen/Shielded/
// FusedWith piece flags. Two positions identical on the board but different
// in, say, active lava squares hashed identically, so the transposition
// table could return a cached score/move from a position the current search
// had never actually seen. overlays.go extends the same incremental XOR
// scheme to every piece of Chess404-specific state as it's added, rather
// than leaving that as a documented-but-fictional TODO the way the previous
// implementation did.

var (
	zobristPieceKeys    [12][64]uint64
	zobristSideKey       uint64
	zobristCastleKeys    [4]uint64 // indexed by the CastleXxx bit position (0=WK,1=WQ,2=BK,3=BQ)
	zobristEnPassantKeys [8]uint64 // indexed by file
)

func init() {
	// Fixed seed: a deterministic hash space means a perft/search run is
	// exactly reproducible across builds and machines, which matters far
	// more for a shared trust anchor like this than any property a
	// non-deterministic seed would buy.
	rng := rand.New(rand.NewSource(0xc0ffee))
	for pieceIdx := 0; pieceIdx < 12; pieceIdx++ {
		for sq := 0; sq < 64; sq++ {
			zobristPieceKeys[pieceIdx][sq] = rng.Uint64()
		}
	}
	zobristSideKey = rng.Uint64()
	for i := range zobristCastleKeys {
		zobristCastleKeys[i] = rng.Uint64()
	}
	for i := range zobristEnPassantKeys {
		zobristEnPassantKeys[i] = rng.Uint64()
	}
}

// pieceZobristKey looks up a single (piece, square) key -- exported at
// package level (lowercase, package-internal) so both this file's full
// recompute and move.go's incremental updates share exactly one source of
// trute for what a piece-on-square contributes to the hash.
func pieceZobristKey(piece Piece, sq Square) uint64 {
	return zobristPieceKeys[piece.Index()][sq]
}

// castleZobristKey returns the key for a single castling-right bit (one of
// the CastleXxx constants) -- callers XOR this in/out as that specific right
// is gained or lost, never as a bulk 4-bit recompute, which is what makes
// the castling contribution incremental too.
func castleZobristKey(right uint8) uint64 {
	switch right {
	case CastleWhiteKingside:
		return zobristCastleKeys[0]
	case CastleWhiteQueenside:
		return zobristCastleKeys[1]
	case CastleBlackKingside:
		return zobristCastleKeys[2]
	case CastleBlackQueenside:
		return zobristCastleKeys[3]
	default:
		return 0
	}
}

func enPassantZobristKey(sq Square) uint64 {
	if sq == NoSquare {
		return 0
	}
	return zobristEnPassantKeys[sq.File()]
}

// computeHash recomputes the zobrist hash for p from scratch. Used to
// initialize Position.hash (FEN parsing, NewStartingPosition,
// NewEmptyPosition) and as the independent oracle incrementalHash is tested
// against -- it must never be called per-node in a real search; that's
// exactly the mistake this package exists to not repeat.
func computeHash(p *Position) uint64 {
	var h uint64
	for idx := 0; idx < 12; idx++ {
		bb := p.pieces[idx]
		for bb.Any() {
			var sq Square
			sq, bb = bb.PopLSB()
			h ^= zobristPieceKeys[idx][sq]
		}
	}
	if p.sideToMove == Black {
		h ^= zobristSideKey
	}
	for _, right := range [4]uint8{CastleWhiteKingside, CastleWhiteQueenside, CastleBlackKingside, CastleBlackQueenside} {
		if p.castling&right != 0 {
			h ^= castleZobristKey(right)
		}
	}
	h ^= enPassantZobristKey(p.enPassant)
	return h
}

// Hash returns the position's current zobrist key, maintained incrementally
// by MakeMove/UnmakeMove.
func (p *Position) Hash() uint64 { return p.hash }
