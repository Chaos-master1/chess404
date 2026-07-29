package core

// Card overlay state: everything Chess404 adds on top of plain chess.
// Position (position.go) deliberately knows none of this -- see its package
// comment. CardOverlay is a separate struct paired alongside a *Position,
// not embedded in it, so plain-chess perft/Zobrist correctness (tasks 14-18)
// stayed checkable against standard reference values with zero Chess404-
// specific interference.
//
// Every mechanic here is modeled directly against internal/match's actual
// behavior (read function-by-function, not inferred from card text or the
// original rebuild plan's high-level sketch -- both turned out to disagree
// with the authoritative implementation in specific, load-bearing ways
// documented per-mechanic below). internal/match remains the source of
// truth; where a comment says "matches X", X is the exact function this was
// read from.
//
// Three genuinely different shapes fall out of reading internal/match, not
// the one uniform "bit plane per effect" the original plan sketched:
//
//  1. Legality/attack-integrated overlays (Frozen, Fortress, FusedWith):
//     these change what a legal-move generator or attack query must return,
//     so they compose with movegen here (see overlays_movegen.go) --
//     "compose with legality instead of being bolted on after it", per
//     bitboard.go's package comment.
//  2. Apply-time interceptors (Shielded): these never change what move is
//     generated as legal -- internal/match happily generates a capture of a
//     shielded piece -- they change what happens when that move is
//     *applied*: the capture is voided and the shield consumed instead.
//     TryConsumeShield below is the primitive a future move-application
//     layer (Phase 2's engine/actions) calls; it is deliberately NOT wired
//     into move generation.
//  3. Pure reactive triggers (Lava, Bomb, BlackHole): zero effect on
//     legality or attacks -- confirmed by an exhaustive read of
//     internal/match/chess.go, which references none of them. They only
//     remove pieces from the board after a move already committed. See
//     overlays_triggers.go for their Resolve/Tick functions.
//
// Fog is deliberately NOT modeled: internal/match/chess.go never references
// FogZones, and match_snapshots.go's filterStateForColor only ever redacts a
// FogZone's own metadata from the opponent's outbound payload -- it has no
// rules effect whatsoever, so a "fog bit plane" would be dead weight in a
// rules kernel.
//
// Pending-card and double-move state (which card is mid-resolution, whether
// the current turn is the first or second half of a double move) are turn-
// sequencing concerns, not board overlays -- they belong to Phase 2's
// engine/actions (the plan's L1, "unified action space"), not this L0
// kernel, and are deliberately left unmodeled here.

// LavaSquare is one active lava trap. Unlike every other zone type, it has
// no owner -- match_cards.go's lavaground case (:910-927) never records who
// cast it, and resolveLavaEffects (:1349-1380) never checks.
type LavaSquare struct {
	Sq        Square
	MovesLeft int
}

// BombTimer tracks one bomb-carrying piece's countdown to detonation,
// independent of the per-square marker bit CardOverlay also keeps (see
// bombMarker's comment) -- mirroring the reference's hybrid
// Piece.Bomb-plus-MatchState.BombPieces split (match_cards.go's unabomber
// case, :1006-1023).
type BombTimer struct {
	Sq        Square
	TurnsLeft int
	Owner     Color
}

// BlackHoleZone is one armed blackhole: two independently chosen squares
// (not necessarily adjacent) that both detonate together once TurnsLeft
// reaches 0. Matches contracts.BlackHoleZone / match_cards.go's blackhole
// case (:818-851).
type BlackHoleZone struct {
	Sq1, Sq2  Square
	TurnsLeft int
	Owner     Color
}

// CardOverlay carries all Chess404 card-effect state that isn't plain chess.
// Construct with NewCardOverlay; the zero value is also valid (every field's
// zero value already means "no effect active", matching a fresh position
// with no cards played yet) but NewCardOverlay is the documented spelling.
type CardOverlay struct {
	// frozen/shielded are per-square boolean planes: Set(sq) means "the
	// piece currently on sq has this status". A vacated square's bit is
	// meaningful only while something occupies it -- callers that move
	// pieces must call MoveOverlay/ClearOverlay to keep it that way, exactly
	// as contracts.Piece's Frozen/Shielded fields implicitly travel with (or
	// vanish with) the piece struct they're embedded in.
	frozen   Bitboard
	shielded Bitboard

	// shieldExpiry[sq] is the FullMoveNumber threshold at which sq's shield
	// expires (contracts.Piece.ShieldTurn) -- meaningless where shielded
	// doesn't have sq set. Deliberately NOT part of Hash(): like
	// Position.halfMoveClock, it's a countdown toward a future state change,
	// not a distinguishing feature of the current one.
	shieldExpiry [64]int

	// fusedWith[sq] is the secondary piece type sq's occupant is fused with
	// (contracts.Piece.FusedWith), or NoPieceType if not fused.
	fusedWith [64]PieceType

	// bombMarker is the per-square mirror of contracts.Piece.Bomb --
	// independent of bombTimers below, exactly like the reference's hybrid
	// representation. This is what lets ResolveBombs (overlays_triggers.go)
	// detect "the tracked square's original carrier was captured/replaced
	// and the timer was never re-pointed there" and fizzle silently instead
	// of exploding whatever piece happens to be there now, matching
	// resolveBombEffects' `piece == nil || !piece.Bomb` check
	// (match_cards.go:1404-1406).
	bombMarker Bitboard

	// fortressMask[c]/hasFortress[c]/fortressExpiry[c] is c's single active
	// Fortress zone (internal/match caps each owner at one, replacing on
	// recast -- match_cards.go's fortress case, :958-977), stored as the
	// already-resolved 4-square bitboard rather than a corner square, since
	// every consumer (movegen, attack queries, Hash) wants the mask.
	fortressMask   [2]Bitboard
	hasFortress    [2]bool
	fortressExpiry [2]int // TurnsLeft; ticked only by the non-owner's move, see TickFortresses

	lavaSquares []LavaSquare
	bombTimers  []BombTimer
	blackHoles  []BlackHoleZone
}

// NewCardOverlay returns an overlay with no active effects.
func NewCardOverlay() *CardOverlay {
	return &CardOverlay{}
}

// Clone returns a deep copy: the bitboard/array fields copy by value
// automatically, but the three zone slices need an explicit copy so
// mutating the clone never aliases the original's backing array.
func (ov *CardOverlay) Clone() *CardOverlay {
	clone := *ov
	clone.lavaSquares = append([]LavaSquare(nil), ov.lavaSquares...)
	clone.bombTimers = append([]BombTimer(nil), ov.bombTimers...)
	clone.blackHoles = append([]BlackHoleZone(nil), ov.blackHoles...)
	return &clone
}

// -- Frozen --------------------------------------------------------------

// IsFrozen reports whether sq's occupant cannot move (contracts.Piece.Frozen).
func (ov *CardOverlay) IsFrozen(sq Square) bool { return ov.frozen.Has(sq) }

// SetFrozen sets or clears sq's frozen bit.
func (ov *CardOverlay) SetFrozen(sq Square, frozen bool) {
	if frozen {
		ov.frozen = ov.frozen.Set(sq)
	} else {
		ov.frozen = ov.frozen.Clear(sq)
	}
}

// -- Shielded --------------------------------------------------------------

// IsShielded reports whether sq's occupant will absorb its next
// capture/removal attempt.
func (ov *CardOverlay) IsShielded(sq Square) bool { return ov.shielded.Has(sq) }

// SetShielded arms sq's shield, expiring at fullMoveNumAtCast+1 -- matches
// match_cards.go:263-265's `shieldTurn := state.FullMoveNum + 1`.
func (ov *CardOverlay) SetShielded(sq Square, fullMoveNumAtCast int) {
	ov.shielded = ov.shielded.Set(sq)
	ov.shieldExpiry[sq] = fullMoveNumAtCast + 1
}

// TryConsumeShield reports whether sq currently carries a shield and, if so,
// consumes it (clears it) and returns true -- matching every capture/removal
// site's `if occupant.Shielded { occupant.Shielded = false; continue/return }`
// pattern (match_actions.go:73-84 and the AoE/direct-removal sites in
// match_cards.go). Callers use this to decide whether a capture/removal
// actually happens; see this file's package comment -- Shield is an
// apply-time interceptor, invisible to legality, so movegen never calls
// this.
func (ov *CardOverlay) TryConsumeShield(sq Square) bool {
	if !ov.shielded.Has(sq) {
		return false
	}
	ov.shielded = ov.shielded.Clear(sq)
	ov.shieldExpiry[sq] = 0
	return true
}

// ExpireShields clears every shield whose FullMoveNumber threshold has been
// reached, matching cleanupTemporaryEffects's `state.FullMoveNum >=
// *piece.ShieldTurn` check (match_cards.go:2069-2071), which runs after
// EVERY move by either color. Because ShieldTurn is cast-time FullMoveNum+1,
// and FullMoveNum only increments after Black's move, a shield cast on
// White's turn survives through Black's reply and expires right after it
// (protects for exactly one opponent move); a shield cast on Black's turn
// has Black's own move immediately bump FullMoveNum to the expiry threshold
// BEFORE this ever runs on White's behalf, so it expires before White gets a
// turn to test it. This asymmetry is real (verified against internal/match's
// code and turn-ordering, not a guess) and is reproduced here deliberately
// rather than "fixed", since the goal is matching what players actually
// experience on the live server.
func (ov *CardOverlay) ExpireShields(fullMoveNum int) {
	remaining := ov.shielded
	for remaining.Any() {
		var sq Square
		sq, remaining = remaining.PopLSB()
		if fullMoveNum >= ov.shieldExpiry[sq] {
			ov.shielded = ov.shielded.Clear(sq)
			ov.shieldExpiry[sq] = 0
		}
	}
}

// -- Fused/FusedWith ---------------------------------------------------

// FusedWith returns sq's secondary piece type, or NoPieceType if sq's
// occupant isn't fused.
func (ov *CardOverlay) FusedWith(sq Square) PieceType { return ov.fusedWith[sq] }

// SetFused tags sq as fused with secondary -- both halffuse and fullfusion
// (match_cards.go:1024-1163) leave the survivor's real Type unchanged and
// only add this tag, except their shared bishop+rook special case, which
// instead becomes a plain queen with no tag at all (call ClearFused for
// that; SetFused is only for the ordinary case).
func (ov *CardOverlay) SetFused(sq Square, secondary PieceType) { ov.fusedWith[sq] = secondary }

// ClearFused removes sq's fusion tag, if any.
func (ov *CardOverlay) ClearFused(sq Square) { ov.fusedWith[sq] = NoPieceType }

// -- Fortress ---------------------------------------------------------

// SetFortress installs c's fortress zone as the 2x2 block with topLeft as
// its lowest-file, lowest-rank corner (clamped on-board), replacing any
// existing zone c owns -- match_cards.go's fortress case never has more than
// one active zone per owner; the new one always replaces the old
// (:958-977).
func (ov *CardOverlay) SetFortress(c Color, topLeft Square, turnsLeft int) {
	file, rank := topLeft.File(), topLeft.Rank()
	var mask Bitboard
	for df := 0; df < 2; df++ {
		for dr := 0; dr < 2; dr++ {
			if Valid(file+df, rank+dr) {
				mask = mask.Set(NewSquare(file+df, rank+dr))
			}
		}
	}
	ov.fortressMask[c] = mask
	ov.hasFortress[c] = true
	ov.fortressExpiry[c] = turnsLeft
}

// HasFortress reports whether c currently has an active fortress zone.
func (ov *CardOverlay) HasFortress(c Color) bool { return ov.hasFortress[c] }

// FortressMask returns c's fortress zone as a bitboard (empty if none).
func (ov *CardOverlay) FortressMask(c Color) Bitboard { return ov.fortressMask[c] }

// ClearFortress removes c's fortress zone, if any.
func (ov *CardOverlay) ClearFortress(c Color) {
	ov.fortressMask[c] = 0
	ov.hasFortress[c] = false
	ov.fortressExpiry[c] = 0
}

// TickFortresses decrements the non-owner-gated countdown, matching
// resolveFortressEffects (match_cards.go:1462-1477): a zone only ticks down
// when the color that just completed a move is NOT its owner, so the
// owner's own moves are free and it takes ~N full rounds (not N plies) to
// expire.
func (ov *CardOverlay) TickFortresses(justMovedColor Color) {
	for _, c := range [2]Color{White, Black} {
		if !ov.hasFortress[c] || c == justMovedColor {
			continue
		}
		ov.fortressExpiry[c]--
		if ov.fortressExpiry[c] <= 0 {
			ov.ClearFortress(c)
		}
	}
}

// fortressBlockMask returns the squares c may not move onto or through, and
// the squares that block c's sliding attack rays -- the same set either way:
// every square inside a Fortress c does not own. See fortressEntryBlocked
// (match_cards.go:1479-1489), pathCrossesFortress (match_actions.go:642-654)
// and isInsideEnemyFortress (chess.go:315-330), which this unifies into one
// mask used two ways (overlays_movegen.go's slider generation augments
// occupancy with it; the attack queries do the same) -- behaviorally
// identical to all three separate reference checks, since no piece can ever
// legally occupy a fortress square it doesn't own, so treating that square
// as permanently occupied-by-a-wall for both movement and ray purposes loses
// nothing.
func (ov *CardOverlay) fortressBlockMask(c Color) Bitboard {
	opp := c.Opposite()
	if ov.hasFortress[opp] {
		return ov.fortressMask[opp]
	}
	return 0
}

// -- Lava/Bomb/BlackHole zone management --------------------------------
// (Resolve/Tick functions that need a live *Position live in
// overlays_triggers.go; these are pure additions to the zone lists.)

// AddLava appends a new lava square -- match_cards.go's lavaground case
// (:910-927) never caps the count (LavaSquare has no owner field at all).
func (ov *CardOverlay) AddLava(sq Square, movesLeft int) {
	ov.lavaSquares = append(ov.lavaSquares, LavaSquare{Sq: sq, MovesLeft: movesLeft})
}

// AddBomb registers a new bomb carrier and marks its per-square marker bit
// -- match_cards.go's unabomber case (:1006-1023), which never checks
// whether the target is already bombed (a piece can be double-bombed).
func (ov *CardOverlay) AddBomb(sq Square, owner Color, turnsLeft int) {
	ov.bombTimers = append(ov.bombTimers, BombTimer{Sq: sq, TurnsLeft: turnsLeft, Owner: owner})
	ov.bombMarker = ov.bombMarker.Set(sq)
}

// FollowBombThroughMove re-points every bomb timer sitting on `from` to
// `to`, matching updateBombTracker (match_cards.go:1382-1394). Safe to call
// for every move, bomb-carrying or not (a no-op if nothing matches). Must be
// paired with moving the marker bit too -- MoveOverlay does both together.
func (ov *CardOverlay) FollowBombThroughMove(from, to Square) {
	for i := range ov.bombTimers {
		if ov.bombTimers[i].Sq == from {
			ov.bombTimers[i].Sq = to
		}
	}
}

// AddBlackHole arms a new blackhole zone -- match_cards.go's blackhole case
// (:818-851), which unlike Fortress/Fog never replaces an owner's existing
// zone, only appends (a player can stack several).
func (ov *CardOverlay) AddBlackHole(sq1, sq2 Square, owner Color, turnsLeft int) {
	ov.blackHoles = append(ov.blackHoles, BlackHoleZone{Sq1: sq1, Sq2: sq2, TurnsLeft: turnsLeft, Owner: owner})
}

// -- Moving pieces around -------------------------------------------------

// moveFlag transfers a single bitboard flag from `from` to `to`, clearing
// both first so a captured piece's flag never leaks onto the mover.
func moveFlag(bb Bitboard, from, to Square) Bitboard {
	had := bb.Has(from)
	bb = bb.Clear(from).Clear(to)
	if had {
		bb = bb.Set(to)
	}
	return bb
}

// MoveOverlay transfers every per-square overlay flag (Frozen, Shielded +
// its expiry, FusedWith, the Bomb marker) from `from` to `to`, and follows
// any bomb timer along with it. Every one of these lives directly on
// contracts.Piece in internal/match, so they simply move with the struct
// when the board mutates (see the package comment); this reproduces that
// for a bitboard-per-flag representation. Use ClearOverlay instead for a
// square that becomes a brand-new piece (promotion) rather than an existing
// one relocating.
func (ov *CardOverlay) MoveOverlay(from, to Square) {
	ov.frozen = moveFlag(ov.frozen, from, to)
	hadShield := ov.shielded.Has(from)
	ov.shielded = moveFlag(ov.shielded, from, to)
	if hadShield {
		ov.shieldExpiry[to] = ov.shieldExpiry[from]
	}
	ov.bombMarker = moveFlag(ov.bombMarker, from, to)
	ov.fusedWith[to] = ov.fusedWith[from]
	ov.fusedWith[from] = NoPieceType
	ov.FollowBombThroughMove(from, to)
}

// ClearOverlay wipes every per-square flag at sq -- used where
// internal/match constructs a brand-new *Piece with none of these fields
// set, most notably promotion (match_actions.go's promote branch builds a
// fresh Piece{Type: promoType, Color: ...} rather than copying the pawn's
// flags forward) and a piece being destroyed outright (lava/bomb/blackhole).
func (ov *CardOverlay) ClearOverlay(sq Square) {
	ov.frozen = ov.frozen.Clear(sq)
	ov.shielded = ov.shielded.Clear(sq)
	ov.shieldExpiry[sq] = 0
	ov.bombMarker = ov.bombMarker.Clear(sq)
	ov.fusedWith[sq] = NoPieceType
}

// moore3x3 returns the 3x3 block of squares centered on sq (including sq
// itself), clamped to the board -- the blast shape both Bomb
// (match_cards.go:1414-1435) and BlackHole (:1500-1524) use identically.
func moore3x3(sq Square) Bitboard {
	file, rank := sq.File(), sq.Rank()
	var mask Bitboard
	for df := -1; df <= 1; df++ {
		for dr := -1; dr <= 1; dr++ {
			if Valid(file+df, rank+dr) {
				mask = mask.Set(NewSquare(file+df, rank+dr))
			}
		}
	}
	return mask
}
