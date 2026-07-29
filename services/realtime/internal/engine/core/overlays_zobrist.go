package core

import "math/rand"

// Zobrist hashing for CardOverlay, extending zobrist.go's plain-chess hash
// to the complete Chess404 position -- "any omission is a wrong-move bug"
// per the engine rebuild plan. Kept in its own file/key-space/init(),
// XORed together with Position.Hash() at the call site (p.Hash()^ov.Hash())
// rather than merged into zobrist.go, so the plain-chess hash stays
// independently checkable against nothing but standard perft positions, the
// same reasoning overlays.go's package comment gives for keeping
// CardOverlay a separate struct from Position.
//
// Deliberately NOT incremental, unlike Position.Hash() -- overlay mutations
// happen far less often per node than piece moves, and correctly un-XORing
// a multiset entry (see the lava/bomb/blackhole comment on Hash below) on
// every Tick/Resolve call is exactly the kind of subtle bookkeeping worth
// deferring until a real search shows it's the bottleneck, matching
// movegen.go's stated Phase 1 philosophy of obvious correctness over
// premature optimization.

const maxOverlayDuration = 8

// durationIndex clamps a duration counter into the fixed key-table range.
// All observed durations in internal/match start at 2 (see the overlay
// research's tick-cadence table); 8 is generous headroom, and clamping
// (rather than wrapping) means an out-of-range value can never collide with
// a small in-range one.
func durationIndex(n int) int {
	if n < 0 {
		return 0
	}
	if n >= maxOverlayDuration {
		return maxOverlayDuration - 1
	}
	return n
}

var (
	zobristFrozenKeys   [64]uint64
	zobristShieldedKeys [64]uint64
	zobristFusedKeys    [64][7]uint64 // [square][PieceType]; index 0 (NoPieceType) is never read

	zobristFortressSquareKeys   [2][64]uint64
	zobristFortressDurationKeys [2][maxOverlayDuration]uint64

	zobristLavaSquareKeys   [64]uint64
	zobristLavaDurationKeys [maxOverlayDuration]uint64

	zobristBombSquareKeys   [64]uint64
	zobristBombDurationKeys [maxOverlayDuration]uint64
	zobristBombOwnerKeys    [2]uint64

	zobristBlackHoleSquareKeys   [64]uint64
	zobristBlackHoleDurationKeys [maxOverlayDuration]uint64
	zobristBlackHoleOwnerKeys    [2]uint64
)

func init() {
	// A second, independent fixed seed from zobrist.go's 0xc0ffee -- the two
	// key sets are only ever combined by XOR-ing the final hashes together,
	// never compared key-by-key, so they have no reason to share a stream.
	rng := rand.New(rand.NewSource(0x404))
	for sq := 0; sq < 64; sq++ {
		zobristFrozenKeys[sq] = rng.Uint64()
		zobristShieldedKeys[sq] = rng.Uint64()
		for pt := 0; pt < 7; pt++ {
			zobristFusedKeys[sq][pt] = rng.Uint64()
		}
		zobristLavaSquareKeys[sq] = rng.Uint64()
		zobristBombSquareKeys[sq] = rng.Uint64()
		zobristBlackHoleSquareKeys[sq] = rng.Uint64()
		zobristFortressSquareKeys[0][sq] = rng.Uint64()
		zobristFortressSquareKeys[1][sq] = rng.Uint64()
	}
	for c := 0; c < 2; c++ {
		zobristBombOwnerKeys[c] = rng.Uint64()
		zobristBlackHoleOwnerKeys[c] = rng.Uint64()
		for d := 0; d < maxOverlayDuration; d++ {
			zobristFortressDurationKeys[c][d] = rng.Uint64()
		}
	}
	for d := 0; d < maxOverlayDuration; d++ {
		zobristLavaDurationKeys[d] = rng.Uint64()
		zobristBombDurationKeys[d] = rng.Uint64()
		zobristBlackHoleDurationKeys[d] = rng.Uint64()
	}
}

// Hash recomputes ov's complete Zobrist contribution from scratch. Combine
// with Position.Hash() (e.g. p.Hash()^ov.Hash()) for a single position-
// identity key suitable for a search transposition table -- deliberately
// stricter than internal/match's own repetition-ruling key (positionKey,
// chess.go:408-438), which excludes all overlay state entirely; a TT must
// never conflate two positions that evaluate differently, while a
// repetition ruling has its own, separately-scoped notion of "the same
// position" that a future repetition-detector should match instead of this.
func (ov *CardOverlay) Hash() uint64 {
	var h uint64

	frozen := ov.frozen
	for frozen.Any() {
		var sq Square
		sq, frozen = frozen.PopLSB()
		h ^= zobristFrozenKeys[sq]
	}
	shielded := ov.shielded
	for shielded.Any() {
		var sq Square
		sq, shielded = shielded.PopLSB()
		h ^= zobristShieldedKeys[sq]
	}
	for sq := Square(0); sq < 64; sq++ {
		if pt := ov.fusedWith[sq]; pt != NoPieceType {
			h ^= zobristFusedKeys[sq][pt]
		}
	}
	for _, c := range [2]Color{White, Black} {
		if !ov.hasFortress[c] {
			continue
		}
		mask := ov.fortressMask[c]
		for mask.Any() {
			var sq Square
			sq, mask = mask.PopLSB()
			h ^= zobristFortressSquareKeys[c][sq]
		}
		h ^= zobristFortressDurationKeys[c][durationIndex(ov.fortressExpiry[c])]
	}

	// Lava/Bomb/BlackHole are MULTISETS, not sets -- internal/match's own
	// case handlers confirm duplicate entries are structurally reachable (a
	// piece can be double-bombed: the unabomber case never checks for an
	// existing bomb on the target, match_cards.go:1006-1023; BlackHole never
	// replaces, only appends, match_cards.go:845-850). Plain XOR-per-entry
	// would let two byte-for-byte-identical entries cancel back to zero
	// contribution, silently hashing "two active traps" the same as "none"
	// -- wraparound ADDITION doesn't have that failure mode (x+x=2x, never 0
	// for nonzero x), so list contributions accumulate with + instead of ^.
	for _, lava := range ov.lavaSquares {
		h += zobristLavaSquareKeys[lava.Sq] + zobristLavaDurationKeys[durationIndex(lava.MovesLeft)]
	}
	for _, bomb := range ov.bombTimers {
		h += zobristBombSquareKeys[bomb.Sq] + zobristBombDurationKeys[durationIndex(bomb.TurnsLeft)] + zobristBombOwnerKeys[bomb.Owner]
	}
	for _, hole := range ov.blackHoles {
		h += zobristBlackHoleSquareKeys[hole.Sq1] + zobristBlackHoleSquareKeys[hole.Sq2] +
			zobristBlackHoleDurationKeys[durationIndex(hole.TurnsLeft)] + zobristBlackHoleOwnerKeys[hole.Owner]
	}
	return h
}
