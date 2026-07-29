package core

// Overlay-aware move generation and attack queries: the legality-integrated
// half of overlays.go's three-way split (see its package comment). Kept in
// a separate file, parallel to movegen.go/attacks_query.go, exactly the way
// attacks_leapers.go/attacks_sliders.go/attacks_query.go are already split
// by concern.
//
// These do not call movegen.go's private per-piece-type generators at all --
// sliders need fortress-augmented occupancy, pawns need a fortress-checked
// landing square, and fusion needs per-square secondary-type generation, so
// this file reimplements the same shapes directly against the exported
// attack-table primitives (RookAttacks, KnightAttacks, PawnAttacks, ...)
// rather than threading an optional overlay parameter through the plain-
// chess file position.go's package comment says must stay chess-only.

// attacksAsType returns the squares a piece on `from` would attack if it
// were type `as`, using the position's real occupancy augmented by
// fortress-ray-blocking (block, from fortressBlockMask). This is exactly
// what internal/match's legalMovesWithFusion/isAttackedWithFusion get by
// cloning the board and retyping one square (chess.go:60-89,
// match_cards.go:1812-1842) -- but attack-pattern generation here is already
// a pure function of (square, type, occupancy), never of what's actually
// recorded at `from`, so no clone is needed: this computes the same result
// directly. byColorForPawn matters only when as==Pawn (a pawn's attack
// pattern depends on which direction is "forward").
func attacksAsType(p *Position, from Square, as PieceType, byColorForPawn Color, block Bitboard) Bitboard {
	switch as {
	case Knight:
		return KnightAttacks(from)
	case King:
		return KingAttacks(from)
	case Pawn:
		return PawnAttacks(from, byColorForPawn)
	case Bishop:
		return BishopAttacks(from, p.OccupiedAll()|block)
	case Rook:
		return RookAttacks(from, p.OccupiedAll()|block)
	case Queen:
		return QueenAttacks(from, p.OccupiedAll()|block)
	default:
		return 0
	}
}

// IsAttackedWithFortress is IsAttacked plus fortress ray-blocking for
// sliding attackers -- matches internal/match's plain (non-fusion)
// isAttacked, which is fortress-aware via clearPath's isInsideEnemyFortress
// check (chess.go:315-330) despite the name giving no hint of it. Several
// card mechanics deliberately use this fusion-BLIND check for their own
// king-safety validation (teleport, jump, swap*, borrow, mindcontrol, clone,
// promote/demote family, sniper/badsniper/parasite removal) rather than the
// stricter IsAttackedWithFusion the core move path uses -- a byte-for-byte-
// faithful Phase 2 action layer must call whichever one internal/match calls
// for that specific mechanic, not always the stricter one.
func IsAttackedWithFortress(p *Position, ov *CardOverlay, sq Square, by Color) bool {
	if PawnAttacks(sq, by.Opposite())&p.PieceBitboard(Pawn, by) != 0 {
		return true
	}
	if KnightAttacks(sq)&p.PieceBitboard(Knight, by) != 0 {
		return true
	}
	if KingAttacks(sq)&p.PieceBitboard(King, by) != 0 {
		return true
	}
	occ := p.OccupiedAll() | ov.fortressBlockMask(by)
	if BishopAttacks(sq, occ)&(p.PieceBitboard(Bishop, by)|p.PieceBitboard(Queen, by)) != 0 {
		return true
	}
	if RookAttacks(sq, occ)&(p.PieceBitboard(Rook, by)|p.PieceBitboard(Queen, by)) != 0 {
		return true
	}
	return false
}

// IsAttackedWithFusion is IsAttackedWithFortress plus fused-piece union
// attack detection -- matches isAttackedWithFusion (match_cards.go:1812-1842):
// checks real types first, then once more per `by`-colored fused piece, as
// if it were its FusedWith type. This is what the core move-legality path
// uses (legalMovesWithFusion's internal king-safety filter), so
// GenerateLegalMovesWithOverlay below calls this, not the fusion-blind
// version.
func IsAttackedWithFusion(p *Position, ov *CardOverlay, sq Square, by Color) bool {
	if IsAttackedWithFortress(p, ov, sq, by) {
		return true
	}
	block := ov.fortressBlockMask(by)
	pieces := p.Occupied(by)
	for pieces.Any() {
		var from Square
		from, pieces = pieces.PopLSB()
		secondary := ov.fusedWith[from]
		if secondary == NoPieceType {
			continue
		}
		if attacksAsType(p, from, secondary, by, block).Has(sq) {
			return true
		}
	}
	return false
}

// InCheckWithFortress and InCheckWithFusion are IsAttackedWithFortress/
// IsAttackedWithFusion specialized to "is c's own king attacked", mirroring
// Position.InCheck().
func InCheckWithFortress(p *Position, ov *CardOverlay, c Color) bool {
	return IsAttackedWithFortress(p, ov, p.KingSquare(c), c.Opposite())
}

func InCheckWithFusion(p *Position, ov *CardOverlay, c Color) bool {
	return IsAttackedWithFusion(p, ov, p.KingSquare(c), c.Opposite())
}

// generateSingleSquareMovesAs generates pseudo-legal destinations for the
// piece on `from`, computed as if it were type `as` -- the single-square
// primitive both bulk piece generation (as == the piece's real type) and
// fusion generation (as == FusedWith) share.
func generateSingleSquareMovesAs(p *Position, c Color, from Square, as PieceType, block Bitboard, dst []Move) []Move {
	if as == Pawn {
		return generateSinglePawnMovesAs(p, c, from, block, dst)
	}
	own := p.Occupied(c)
	targets := attacksAsType(p, from, as, c, block) &^ own &^ block
	for targets.Any() {
		var to Square
		to, targets = targets.PopLSB()
		dst = append(dst, Move{From: from, To: to})
	}
	return dst
}

// generateSinglePawnMovesAs generates a pawn's pushes/double-push/captures/
// en-passant from `from`, with fortress landing checks on every destination
// EXCEPT a double push's intermediate square: pawns are never sliders, so
// pathCrossesFortress's isSlider-only guard (match_actions.go:642-654) never
// applies to them -- only the final landing square is checked (via
// fortressEntryBlocked, which applies to every move regardless of piece
// type). This is a deliberate, faithful-to-the-reference asymmetry, not an
// oversight: a pawn can double-push OVER an enemy fortress square it could
// never legally stop ON.
func generateSinglePawnMovesAs(p *Position, c Color, from Square, block Bitboard, dst []Move) []Move {
	occ := p.OccupiedAll()
	enemy := p.Occupied(c.Opposite())
	forward := north
	startRank, promoRank := 1, 7
	if c == Black {
		forward = south
		startRank, promoRank = 6, 0
	}

	oneStep := forward(from.Bit())
	if oneStep&occ == 0 && oneStep != 0 {
		to := oneStep.LSB()
		if !block.Has(to) {
			dst = appendPawnMove(dst, from, to, promoRank, Quiet)
		}
		if from.Rank() == startRank {
			twoStep := forward(oneStep)
			if twoStep&occ == 0 && twoStep != 0 {
				to2 := twoStep.LSB()
				if !block.Has(to2) {
					dst = append(dst, Move{From: from, To: to2, Flag: DoublePawnPush})
				}
			}
		}
	}

	captures := PawnAttacks(from, c) &^ block
	targets := captures & enemy
	for targets.Any() {
		var to Square
		to, targets = targets.PopLSB()
		dst = appendPawnMove(dst, from, to, promoRank, Quiet)
	}
	if p.enPassant != NoSquare && captures.Has(p.enPassant) {
		dst = append(dst, Move{From: from, To: p.enPassant, Flag: EnPassantCapture})
	}
	return dst
}

func generatePawnMovesOverlay(p *Position, c Color, block Bitboard, dst []Move) []Move {
	pawns := p.PieceBitboard(Pawn, c)
	for pawns.Any() {
		var from Square
		from, pawns = pawns.PopLSB()
		dst = generateSinglePawnMovesAs(p, c, from, block, dst)
	}
	return dst
}

// generatePieceMovesOverlay bulk-generates one non-pawn piece type's moves,
// fortress-aware (landing exclusion for all, ray-augmented occupancy for
// sliders via generateSingleSquareMovesAs/attacksAsType).
func generatePieceMovesOverlay(p *Position, c Color, pt PieceType, block Bitboard, dst []Move) []Move {
	pieces := p.PieceBitboard(pt, c)
	for pieces.Any() {
		var from Square
		from, pieces = pieces.PopLSB()
		dst = generateSingleSquareMovesAs(p, c, from, pt, block, dst)
	}
	return dst
}

// generateCastlesOverlay mirrors movegen.go's generateCastles, but validates
// pass-through/landing safety with IsAttackedWithFortress instead of plain
// IsAttacked, and additionally excludes the king's FINAL square (not the
// pass-through square) if it's fortress-blocked. This reproduces a genuine
// gap confirmed in internal/match: fortressEntryBlocked checks the king's
// landing square like any other move's destination, but the pass-through
// square is only ever checked for "attacked", and being inside a fortress
// isn't itself "attacked" -- so a king CAN legally castle through (not onto)
// a square that sits inside an enemy fortress.
func generateCastlesOverlay(p *Position, ov *CardOverlay, c Color, block Bitboard, dst []Move) []Move {
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
		if !occ.Has(passSquares[0]) && !occ.Has(passSquares[1]) && !block.Has(passSquares[1]) &&
			!IsAttackedWithFortress(p, ov, kingFrom, enemy) &&
			!IsAttackedWithFortress(p, ov, passSquares[0], enemy) &&
			!IsAttackedWithFortress(p, ov, passSquares[1], enemy) {
			dst = append(dst, Move{From: kingFrom, To: passSquares[1], Flag: CastleKingside})
		}
	}
	if p.HasCastleRight(queenside) {
		passSquares := [2]Square{NewSquare(3, rank), NewSquare(2, rank)}
		knightSquare := NewSquare(1, rank)
		if !occ.Has(passSquares[0]) && !occ.Has(passSquares[1]) && !occ.Has(knightSquare) && !block.Has(passSquares[1]) &&
			!IsAttackedWithFortress(p, ov, kingFrom, enemy) &&
			!IsAttackedWithFortress(p, ov, passSquares[0], enemy) &&
			!IsAttackedWithFortress(p, ov, passSquares[1], enemy) {
			dst = append(dst, Move{From: kingFrom, To: passSquares[1], Flag: CastleQueenside})
		}
	}
	return dst
}

// generateFusionMoves unions in each fused piece's secondary-type moves,
// matching legalMovesWithFusion's "generate once as the real type [already
// done by generatePawnMovesOverlay/generatePieceMovesOverlay above], once
// more retyped to FusedWith, union" pattern (chess.go:60-89). Only pieces
// with a non-empty FusedWith tag contribute here.
func generateFusionMoves(p *Position, ov *CardOverlay, c Color, block Bitboard, dst []Move) []Move {
	pieces := p.Occupied(c)
	for pieces.Any() {
		var from Square
		from, pieces = pieces.PopLSB()
		secondary := ov.fusedWith[from]
		if secondary == NoPieceType {
			continue
		}
		dst = generateSingleSquareMovesAs(p, c, from, secondary, block, dst)
	}
	return dst
}

func generatePseudoMovesWithOverlay(p *Position, ov *CardOverlay, dst []Move) []Move {
	c := p.sideToMove
	block := ov.fortressBlockMask(c)

	dst = generatePawnMovesOverlay(p, c, block, dst)
	dst = generatePieceMovesOverlay(p, c, Knight, block, dst)
	dst = generatePieceMovesOverlay(p, c, Bishop, block, dst)
	dst = generatePieceMovesOverlay(p, c, Rook, block, dst)
	dst = generatePieceMovesOverlay(p, c, Queen, block, dst)
	dst = generatePieceMovesOverlay(p, c, King, block, dst)
	dst = generateCastlesOverlay(p, ov, c, block, dst)
	dst = generateFusionMoves(p, ov, c, block, dst)
	return dst
}

// GenerateLegalMovesWithOverlay is GenerateLegalMoves plus fortress- and
// fusion-aware generation/king-safety, matching legalMovesWithFusion
// (chess.go:60-89) exactly -- including being Frozen-BLIND, on purpose: the
// reference has zero Frozen references anywhere in its movegen, so this
// does too. Use GenerateSubmittableMoves for what a player may actually
// submit, and TerminalStatusWithOverlay for checkmate/stalemate
// classification; both are built on top of this, deliberately not the other
// way around.
//
// The king-safety filter below never needs to sync ov during its make/
// unmake probing: it only ever queries the OPPONENT's pieces
// (IsAttackedWithFusion(..., mover.Opposite())), and a mover's own move can
// never relocate an opponent's piece (a capture only ever removes one, which
// simply drops that square out of the opponent's occupancy bitboard the
// query already reads live) -- so any overlay state at a square that
// changed occupancy is naturally never consulted, whether or not ov itself
// was updated to match.
func GenerateLegalMovesWithOverlay(p *Position, ov *CardOverlay) []Move {
	pseudo := generatePseudoMovesWithOverlay(p, ov, make([]Move, 0, 64))
	legal := pseudo[:0]
	mover := p.sideToMove
	for _, m := range pseudo {
		u := p.MakeMove(m)
		if !IsAttackedWithFusion(p, ov, p.KingSquare(mover), mover.Opposite()) {
			legal = append(legal, m)
		}
		p.UnmakeMove(u)
	}
	return legal
}

// GenerateSubmittableMoves returns the moves the side to move may actually
// submit right now: GenerateLegalMovesWithOverlay filtered to exclude any
// move whose mover is Frozen (contracts.Piece.Frozen blocks moving outright,
// checked before movegen even runs in internal/match -- match_actions.go:
// 42-44). Deliberately NOT used for checkmate/stalemate classification --
// see TerminalStatusWithOverlay.
func GenerateSubmittableMoves(p *Position, ov *CardOverlay) []Move {
	legal := GenerateLegalMovesWithOverlay(p, ov)
	submittable := legal[:0]
	for _, m := range legal {
		if !ov.IsFrozen(m.From) {
			submittable = append(submittable, m)
		}
	}
	return submittable
}

// GameStatus classifies a position the way internal/match's
// gameStatusWithFusion does.
type GameStatus int

const (
	Ongoing GameStatus = iota
	Checkmate
	Stalemate
)

// TerminalStatusWithOverlay classifies the side to move's position exactly
// as internal/match's gameStatusWithFusion/hasLegalMoveWithFusion do
// (chess.go:91-119, :154-163): fortress- and fusion-aware, but deliberately
// Frozen-BLIND -- the reference iterates every piece of the side to move
// with no Frozen check at all, so a position where the only pseudo-legal
// moves belong to a frozen piece is NOT stalemate on the live server. This
// function must agree, or engine/conform (Task 20) will correctly flag a
// divergence.
func TerminalStatusWithOverlay(p *Position, ov *CardOverlay) GameStatus {
	if len(GenerateLegalMovesWithOverlay(p, ov)) > 0 {
		return Ongoing
	}
	if InCheckWithFusion(p, ov, p.sideToMove) {
		return Checkmate
	}
	return Stalemate
}
