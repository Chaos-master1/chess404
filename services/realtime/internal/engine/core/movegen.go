package core

// Move generation: pseudo-legal moves per piece type, built entirely from
// the attack tables in attacks_leapers.go/attacks_sliders.go, plus a legal
// filter. This deliberately favors obvious correctness (generate
// pseudo-legal, then filter by "does this leave my own king in check" via a
// real make/unmake) over the faster pin-aware generation many engines use --
// Phase 1's job is a kernel that perft-verifies exactly against the known
// reference values and against internal/match (engine/conform), not yet the
// fastest possible one. Phase 2 can special-case pinned pieces once there's
// a benchmark showing this filter is the bottleneck; premature pin detection
// here would be exactly the kind of unverified cleverness the previous
// engine's array-board movegen already had too much of.

// GenerateMoves appends every pseudo-legal move for the side to move to dst
// and returns the extended slice, so callers can reuse a buffer across many
// calls (the standard append-into-caller-slice pattern to avoid an
// allocation per node once this is on a search hot path).
func GenerateMoves(p *Position, dst []Move) []Move {
	c := p.sideToMove
	dst = generatePawnMoves(p, c, dst)
	dst = generateLeaperMoves(p, c, Knight, KnightAttacks, dst)
	dst = generateSliderMoves(p, c, Bishop, dst)
	dst = generateSliderMoves(p, c, Rook, dst)
	dst = generateSliderMoves(p, c, Queen, dst)
	dst = generateLeaperMoves(p, c, King, KingAttacks, dst)
	dst = generateCastles(p, c, dst)
	return dst
}

// GenerateLegalMoves returns only the moves that don't leave the mover's own
// king in check. It generates pseudo-legal moves once, then make/unmake-tests
// each -- the O(moves) worst case this implies is exactly what a
// magic-bitboard core buys back overhead for elsewhere (attack queries, the
// actual expensive part of the check test, are now O(1) each).
func GenerateLegalMoves(p *Position) []Move {
	pseudo := GenerateMoves(p, make([]Move, 0, 48))
	legal := pseudo[:0]
	mover := p.sideToMove
	for _, m := range pseudo {
		u := p.MakeMove(m)
		if !p.IsAttacked(p.KingSquare(mover), mover.Opposite()) {
			legal = append(legal, m)
		}
		p.UnmakeMove(u)
	}
	return legal
}

func generateLeaperMoves(p *Position, c Color, pt PieceType, attacksFn func(Square) Bitboard, dst []Move) []Move {
	pieces := p.PieceBitboard(pt, c)
	own := p.Occupied(c)
	for pieces.Any() {
		var from Square
		from, pieces = pieces.PopLSB()
		targets := attacksFn(from) &^ own
		for targets.Any() {
			var to Square
			to, targets = targets.PopLSB()
			dst = append(dst, Move{From: from, To: to})
		}
	}
	return dst
}

func generateSliderMoves(p *Position, c Color, pt PieceType, dst []Move) []Move {
	pieces := p.PieceBitboard(pt, c)
	own := p.Occupied(c)
	occ := p.OccupiedAll()
	for pieces.Any() {
		var from Square
		from, pieces = pieces.PopLSB()
		var attacks Bitboard
		switch pt {
		case Bishop:
			attacks = BishopAttacks(from, occ)
		case Rook:
			attacks = RookAttacks(from, occ)
		case Queen:
			attacks = QueenAttacks(from, occ)
		}
		targets := attacks &^ own
		for targets.Any() {
			var to Square
			to, targets = targets.PopLSB()
			dst = append(dst, Move{From: from, To: to})
		}
	}
	return dst
}

var promotionPieces = [4]PieceType{Knight, Bishop, Rook, Queen}

func generatePawnMoves(p *Position, c Color, dst []Move) []Move {
	pawns := p.PieceBitboard(Pawn, c)
	occ := p.OccupiedAll()
	enemy := p.Occupied(c.Opposite())

	forward := north
	startRank, promoRank := 1, 7
	if c == Black {
		forward = south
		startRank, promoRank = 6, 0
	}

	for pawns.Any() {
		var from Square
		from, pawns = pawns.PopLSB()

		// Single push.
		oneStep := forward(from.Bit())
		if oneStep&occ == 0 && oneStep != 0 {
			to := oneStep.LSB()
			dst = appendPawnMove(dst, from, to, promoRank, Quiet)

			// Double push, only from the start rank, only if BOTH squares in
			// front are empty (oneStep already confirmed empty above).
			if from.Rank() == startRank {
				twoStep := forward(oneStep)
				if twoStep&occ == 0 && twoStep != 0 {
					dst = append(dst, Move{From: from, To: twoStep.LSB(), Flag: DoublePawnPush})
				}
			}
		}

		// Captures, including en passant.
		captures := PawnAttacks(from, c)
		targets := captures & enemy
		for targets.Any() {
			var to Square
			to, targets = targets.PopLSB()
			dst = appendPawnMove(dst, from, to, promoRank, Quiet)
		}
		if p.enPassant != NoSquare && captures.Has(p.enPassant) {
			dst = append(dst, Move{From: from, To: p.enPassant, Flag: EnPassantCapture})
		}
	}
	return dst
}

// appendPawnMove appends a single pawn push/capture, expanding it into all
// four promotion choices if the destination is the far rank.
func appendPawnMove(dst []Move, from, to Square, promoRank int, flag MoveFlag) []Move {
	if to.Rank() == promoRank {
		for _, promo := range promotionPieces {
			dst = append(dst, Move{From: from, To: to, Promotion: promo, Flag: flag})
		}
		return dst
	}
	return append(dst, Move{From: from, To: to, Flag: flag})
}

// generateCastles appends the (at most two) castling moves available to c,
// checking every one of the standard three preconditions: the relevant
// rights are held, the squares between king and rook are empty, and the
// king does not start in, pass through, or land on an attacked square. That
// last check is the one the previous array-board engine's castling
// generator got wrong (services/realtime/internal/engine/chess.go:283 --
// see the engine audit -- it derived the "home rank" from the king's
// CURRENT square instead of validating it hadn't moved via castling rights,
// so a king that had walked elsewhere could still "castle" through it).
// Here, castling rights themselves (cleared the instant a king or rook
// leaves its home square -- see move.go's castlingRightsClearedBy) are the
// only source of truth for whether the king/rook are still on their home
// squares; there is no separate, potentially-inconsistent position check.
func generateCastles(p *Position, c Color, dst []Move) []Move {
	rank := 0
	kingside, queenside := CastleWhiteKingside, CastleWhiteQueenside
	if c == Black {
		rank = 7
		kingside, queenside = CastleBlackKingside, CastleBlackQueenside
	}
	kingFrom := NewSquare(4, rank)
	occ := p.OccupiedAll()
	enemy := c.Opposite()

	if p.HasCastleRight(kingside) {
		passSquares := [2]Square{NewSquare(5, rank), NewSquare(6, rank)}
		if !occ.Has(passSquares[0]) && !occ.Has(passSquares[1]) &&
			!p.IsAttacked(kingFrom, enemy) && !p.IsAttacked(passSquares[0], enemy) && !p.IsAttacked(passSquares[1], enemy) {
			dst = append(dst, Move{From: kingFrom, To: passSquares[1], Flag: CastleKingside})
		}
	}
	if p.HasCastleRight(queenside) {
		passSquares := [2]Square{NewSquare(3, rank), NewSquare(2, rank)}
		knightSquare := NewSquare(1, rank) // must be empty too, but is never attack-checked (the king never crosses it)
		if !occ.Has(passSquares[0]) && !occ.Has(passSquares[1]) && !occ.Has(knightSquare) &&
			!p.IsAttacked(kingFrom, enemy) && !p.IsAttacked(passSquares[0], enemy) && !p.IsAttacked(passSquares[1], enemy) {
			dst = append(dst, Move{From: kingFrom, To: passSquares[1], Flag: CastleQueenside})
		}
	}
	return dst
}
