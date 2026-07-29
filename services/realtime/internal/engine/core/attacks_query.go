package core

// IsAttacked reports whether sq is attacked by any piece of color `by`, given
// the position's current occupancy. This is the single query check
// detection, castling safety, and legal-move filtering all reduce to.
func (p *Position) IsAttacked(sq Square, by Color) bool {
	occ := p.occupiedAll

	// PawnAttacks(sq, by.Opposite()) computes the squares a pawn of the
	// OPPOSITE color standing on sq would attack -- which is exactly the set
	// of squares an actual `by`-colored pawn would need to occupy to attack
	// sq (pawn attack is being queried in reverse: "who could be attacking
	// me" rather than "what do I attack").
	if PawnAttacks(sq, by.Opposite())&p.PieceBitboard(Pawn, by) != 0 {
		return true
	}
	if KnightAttacks(sq)&p.PieceBitboard(Knight, by) != 0 {
		return true
	}
	if KingAttacks(sq)&p.PieceBitboard(King, by) != 0 {
		return true
	}
	bishopsAndQueens := p.PieceBitboard(Bishop, by) | p.PieceBitboard(Queen, by)
	if BishopAttacks(sq, occ)&bishopsAndQueens != 0 {
		return true
	}
	rooksAndQueens := p.PieceBitboard(Rook, by) | p.PieceBitboard(Queen, by)
	if RookAttacks(sq, occ)&rooksAndQueens != 0 {
		return true
	}
	return false
}

// KingSquare returns the square of c's king. Panics-by-returning-NoSquare if
// somehow absent (a position missing a king is a caller bug -- every real
// chess position has exactly one of each), so callers get an obviously wrong
// downstream value to debug rather than a silent panic deep in bit math.
func (p *Position) KingSquare(c Color) Square {
	return p.PieceBitboard(King, c).LSB()
}

// InCheck reports whether the side to move's king is currently attacked.
func (p *Position) InCheck() bool {
	return p.IsAttacked(p.KingSquare(p.sideToMove), p.sideToMove.Opposite())
}
