package core

// Precomputed attack tables for the three "leaper" piece types -- knight,
// king, and pawn -- whose attack sets depend only on the piece's own square
// (and, for pawns, its color), never on what else is on the board. Each is a
// fixed 64-entry (or 2x64 for pawns) table built once at package init and
// then just indexed, which is the entire point of a bitboard engine: this
// table lookup replaces what the old array-board engine did by re-deriving
// candidate deltas and bounds-checking them on every single call, for every
// piece, at every search node (services/realtime/internal/engine/chess.go's
// pseudoMoves).

var (
	knightAttacks [64]Bitboard
	kingAttacks   [64]Bitboard
	// pawnAttacks[color][square] -- diagonal capture squares only, not the
	// forward push (pushes depend on occupancy, so movegen handles them
	// separately). color 0 = white, 1 = black, matching this package's
	// Color type.
	pawnAttacks [2][64]Bitboard
)

func init() {
	for sq := Square(0); sq < 64; sq++ {
		knightAttacks[sq] = computeKnightAttacks(sq)
		kingAttacks[sq] = computeKingAttacks(sq)
		pawnAttacks[White][sq] = computePawnAttacks(sq, White)
		pawnAttacks[Black][sq] = computePawnAttacks(sq, Black)
	}
}

// KnightAttacks returns the knight's attack set from sq, ignoring occupancy
// (a knight's attack set never depends on what's on the board -- only
// whether the destination happens to hold a friendly piece, which callers
// mask out themselves).
func KnightAttacks(sq Square) Bitboard { return knightAttacks[sq] }

// KingAttacks returns the king's one-step attack set from sq (castling is a
// distinct, occupancy- and rights-dependent move generated separately).
func KingAttacks(sq Square) Bitboard { return kingAttacks[sq] }

// PawnAttacks returns the diagonal capture squares (not the forward push)
// for a pawn of the given color on sq.
func PawnAttacks(sq Square, c Color) Bitboard { return pawnAttacks[c][sq] }

func computeKnightAttacks(sq Square) Bitboard {
	b := sq.Bit()
	// The 8 knight deltas, each expressed as two single-step shifts so the
	// existing file-wrap masking on those primitives does the edge-safety
	// work for us instead of duplicating it with delta/bounds arithmetic.
	var attacks Bitboard
	attacks |= north(north(east(b)))
	attacks |= north(north(west(b)))
	attacks |= south(south(east(b)))
	attacks |= south(south(west(b)))
	attacks |= east(east(north(b)))
	attacks |= east(east(south(b)))
	attacks |= west(west(north(b)))
	attacks |= west(west(south(b)))
	return attacks
}

func computeKingAttacks(sq Square) Bitboard {
	b := sq.Bit()
	var attacks Bitboard
	attacks |= north(b)
	attacks |= south(b)
	attacks |= east(b)
	attacks |= west(b)
	attacks |= northEast(b)
	attacks |= northWest(b)
	attacks |= southEast(b)
	attacks |= southWest(b)
	return attacks
}

func computePawnAttacks(sq Square, c Color) Bitboard {
	b := sq.Bit()
	if c == White {
		return northEast(b) | northWest(b)
	}
	return southEast(b) | southWest(b)
}
