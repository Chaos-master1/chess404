package core

// The reactive-trigger half of overlays.go's three-way split: functions
// that need a live *Position (not just *CardOverlay) because they read or
// mutate actual piece occupancy -- Frozen thawing and the three pure-AoE
// mechanics (Lava, Bomb, BlackHole), none of which affect legality or
// attacks (see overlays.go's package comment).
//
// None of these decide when a "turn" has ended -- that's Phase 2's
// engine/actions concern (a double-move's first half explicitly does NOT
// trigger any of these, matching internal/match's early return in
// applyMove, match_actions.go:124-169). Each function here corresponds to
// exactly one internal/match resolve*/cleanup* function, callable
// independently once a future action layer knows a whole turn has
// completed.

// ThawAfterMove clears Frozen from every square moverColor currently
// occupies, matching cleanupTemporaryEffects's `piece.Frozen && piece.Color
// == justMovedColor` (match_cards.go:2065-2067): a frozen piece thaws the
// instant its own side completes its next move -- any piece, not
// necessarily the frozen one (it can't move; see GenerateSubmittableMoves).
func ThawAfterMove(p *Position, ov *CardOverlay, moverColor Color) {
	ov.frozen &^= p.Occupied(moverColor)
}

// blastSquares applies an AoE removal over every square in mask: non-king,
// non-shielded occupants are removed (a shielded occupant consumes its
// shield and survives instead); empty squares and kings are untouched.
// Shared by ResolveLava/ResolveBombs/TickBlackHoles, which all use this
// identical destroy/shield-absorb/king-immune rule (match_cards.go:1362-1364
// lava, :1425-1429 bomb, :1510-1514 blackhole). Returns every square whose
// occupant was actually removed.
func blastSquares(p *Position, ov *CardOverlay, mask Bitboard) []Square {
	var cleared []Square
	for mask.Any() {
		var sq Square
		sq, mask = mask.PopLSB()
		occupant := p.PieceAt(sq)
		if occupant.IsNone() || occupant.Type == King {
			continue
		}
		if ov.TryConsumeShield(sq) {
			continue
		}
		p.removePiece(sq, occupant)
		ov.ClearOverlay(sq)
		cleared = append(cleared, sq)
	}
	return cleared
}

// ResolveLava applies every consequence of a move landing on `landing`,
// matching resolveLavaEffects (match_cards.go:1349-1380) exactly: every
// OTHER lava square ticks down by one, unconditionally, regardless of color
// (dropped once MovesLeft reaches 0), and if `landing` itself held a trap,
// its occupant is destroyed via blastSquares (king-immune, shield-absorbed)
// and the trap is consumed either way (triggered or not, a trap is always
// one-shot). Returns the square actually cleared, if any.
func ResolveLava(p *Position, ov *CardOverlay, landing Square) []Square {
	kept := ov.lavaSquares[:0]
	triggered := false
	for _, lava := range ov.lavaSquares {
		if lava.Sq == landing {
			triggered = true
			continue
		}
		lava.MovesLeft--
		if lava.MovesLeft > 0 {
			kept = append(kept, lava)
		}
	}
	ov.lavaSquares = kept
	if !triggered {
		return nil
	}
	return blastSquares(p, ov, landing.Bit())
}

// ResolveBombs ticks every bomb timer down by one, unconditionally (matches
// resolveBombEffects, match_cards.go:1396-1443, called after every single
// ply regardless of color -- the same cadence as lava, unlike Fortress/
// BlackHole's opponent-move-gated cadence). A timer reaching 0 detonates a
// moore3x3 blast around its tracked square -- but only if the marker bit
// confirms the original carrier is still actually there (see
// FollowBombThroughMove / bombMarker's doc comment on CardOverlay);
// otherwise it fizzles silently, matching `piece == nil || !piece.Bomb`
// (match_cards.go:1404-1406). Returns every square cleared, across every
// bomb that detonated this call.
func ResolveBombs(p *Position, ov *CardOverlay) []Square {
	var cleared []Square
	kept := ov.bombTimers[:0]
	for _, bomb := range ov.bombTimers {
		bomb.TurnsLeft--
		if bomb.TurnsLeft > 0 {
			kept = append(kept, bomb)
			continue
		}
		if ov.bombMarker.Has(bomb.Sq) {
			ov.bombMarker = ov.bombMarker.Clear(bomb.Sq)
			cleared = append(cleared, blastSquares(p, ov, moore3x3(bomb.Sq))...)
		}
	}
	ov.bombTimers = kept
	return cleared
}

// TickBlackHoles applies resolveBlackHoleEffects' opponent-move-gated
// cadence (match_cards.go:1491-1540, same as Fortress: a zone only ticks
// down when the color that just moved is not its owner). A zone reaching 0
// detonates BOTH its squares' moore3x3 blasts (a single Bitboard union
// naturally dedupes any overlap, matching the reference's explicit `seen`
// map). Returns every square cleared, across every zone that detonated this
// call.
func TickBlackHoles(p *Position, ov *CardOverlay, justMovedColor Color) []Square {
	var cleared []Square
	kept := ov.blackHoles[:0]
	for _, hole := range ov.blackHoles {
		if hole.Owner != justMovedColor {
			hole.TurnsLeft--
		}
		if hole.TurnsLeft > 0 {
			kept = append(kept, hole)
			continue
		}
		cleared = append(cleared, blastSquares(p, ov, moore3x3(hole.Sq1)|moore3x3(hole.Sq2))...)
	}
	ov.blackHoles = kept
	return cleared
}

// MakeMoveWithOverlay applies m to p (via the plain Position.MakeMove) and
// keeps ov's per-square flags consistent with the piece that just moved:
// captured-square flags are discarded, the mover's own flags travel with
// it, castling moves the rook's flags too, en passant clears the captured
// pawn's square, and promotion clears the pawn's flags rather than carrying
// them onto the freshly-promoted piece (matching internal/match's promote
// branch, which constructs a brand-new Piece{} rather than copying the
// pawn's fields forward).
//
// This does NOT run Frozen-thaw, Shield-expiry, or any zone tick --
// those are gated on which color's TURN just ended, not on the mechanics of
// one single move (a double-move's first half explicitly does not trigger
// them), so they belong to the not-yet-built turn/action layer (Phase 2)
// that knows whether a whole turn just completed. Card mechanics that
// relocate a piece via something other than a normal move (teleport, swap,
// clone, ...) are equally out of scope here -- those are Phase 2
// engine/actions concerns, each with its own bespoke legality rules per the
// overlay research, not a "move" in this package's sense at all.
func MakeMoveWithOverlay(p *Position, ov *CardOverlay, m Move) undo {
	moverColor := p.sideToMove
	if m.IsPromotion() {
		ov.ClearOverlay(m.From)
		ov.ClearOverlay(m.To)
	} else {
		ov.MoveOverlay(m.From, m.To)
	}
	if m.Flag == CastleKingside || m.Flag == CastleQueenside {
		rookFrom, rookTo := castleRookSquares(moverColor, m.Flag)
		ov.MoveOverlay(rookFrom, rookTo)
	}
	if m.Flag == EnPassantCapture {
		capSq := NewSquare(m.To.File(), m.From.Rank())
		ov.ClearOverlay(capSq)
	}
	return p.MakeMove(m)
}
