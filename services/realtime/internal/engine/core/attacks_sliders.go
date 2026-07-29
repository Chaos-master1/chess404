package core

import "math/rand"

// Magic bitboards for the sliding pieces (rook, bishop; queen = both
// combined). This is the single biggest speed lever in the rebuild: the
// previous engine's `pathCrossesFortress`/`clearPath`/`legalMoves` traced
// rays square-by-square with bounds checks on an array board, and rebuilt
// that trace from scratch at every call, for every candidate move, at every
// search node. A magic bitboard turns "is this ray blocked, and where" into
// one multiply, one shift, and one array lookup, with the blocker-blocked
// attack pattern for every possible occupancy already precomputed.
//
// How it works, briefly: for a rook (or bishop) on square sq, only the
// occupancy of squares strictly between it and the edge along its own rays
// can possibly change its attack set (an edge square never blocks anything
// further, since the ray stops at the edge regardless). That's the "relevant
// occupancy mask" -- at most 12 bits for a rook, 9 for a bishop. Index every
// possible value of (real board occupancy & relevant mask) through a
// perfect hash `(occupancy * magic) >> shift` into a small precomputed table
// of attack bitboards, one entry per distinct relevant-occupancy pattern.
// Finding a magic number that hashes perfectly (no two different occupancy
// patterns mapping to the same index) is a randomized search, done once at
// package init -- typically a handful of attempts per square, using sparse
// random numbers as candidates.

const (
	rookDirCount   = 4
	bishopDirCount = 4
)

var rookDirs = [rookDirCount][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
var bishopDirs = [bishopDirCount][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

// slidingAttacksSlow ray-traces in the given directions from sq, stopping at
// (and including) the first occupied square in each direction, or the board
// edge. This is the ground truth every magic table entry is verified
// against -- deliberately simple and obviously correct rather than fast, so
// it can serve as the independent oracle for the magic search and for tests.
func slidingAttacksSlow(sq Square, occupancy Bitboard, dirs [][2]int) Bitboard {
	var attacks Bitboard
	file, rank := sq.File(), sq.Rank()
	for _, d := range dirs {
		f, r := file+d[0], rank+d[1]
		for Valid(f, r) {
			target := NewSquare(f, r)
			attacks = attacks.Set(target)
			if occupancy.Has(target) {
				break // first blocker stops the ray (it's capturable, hence included above, but nothing behind it is reachable)
			}
			f += d[0]
			r += d[1]
		}
	}
	return attacks
}

func rookAttacksSlow(sq Square, occupancy Bitboard) Bitboard {
	return slidingAttacksSlow(sq, occupancy, rookDirs[:])
}

func bishopAttacksSlow(sq Square, occupancy Bitboard) Bitboard {
	return slidingAttacksSlow(sq, occupancy, bishopDirs[:])
}

// relevantMask computes the "occupancy that can possibly matter" mask: every
// square strictly between sq and the edge along each direction, excluding
// the edge square itself (a piece sitting on the edge already stops the ray
// there regardless of what's "past" it, so its presence/absence never
// changes the attack set -- excluding it shrinks the table without losing
// any information the attack set depends on).
func relevantMask(sq Square, dirs [][2]int) Bitboard {
	var mask Bitboard
	file, rank := sq.File(), sq.Rank()
	for _, d := range dirs {
		f, r := file+d[0], rank+d[1]
		for {
			nf, nr := f+d[0], r+d[1]
			if !Valid(nf, nr) {
				break // (f,r) is the last square before the edge in this direction
			}
			mask = mask.Set(NewSquare(f, r))
			f, r = nf, nr
		}
	}
	return mask
}

type magicEntry struct {
	mask   Bitboard
	magic  uint64
	shift  uint
	table  []Bitboard
}

var rookMagicTable [64]magicEntry
var bishopMagicTable [64]magicEntry

func init() {
	rng := rand.New(rand.NewSource(0x404c4553534c455)) // fixed seed: deterministic magics, reproducible builds
	for sq := Square(0); sq < 64; sq++ {
		rookMagicTable[sq] = buildMagicEntry(sq, rookDirs[:], rng)
		bishopMagicTable[sq] = buildMagicEntry(sq, bishopDirs[:], rng)
	}
}

// buildMagicEntry finds a working magic number for sq and builds its
// complete attack table. Every distinct value the relevant-occupancy mask
// can take is enumerated (via the standard subset-enumeration trick), the
// slow ray tracer computes the ground-truth attack set for each, and
// candidate magics are tried until every one of those (occupancy, attack)
// pairs hashes into the table with no collisions.
func buildMagicEntry(sq Square, dirs [][2]int, rng *rand.Rand) magicEntry {
	mask := relevantMask(sq, dirs)
	bits := mask.PopCount()
	shift := uint(64 - bits)
	size := 1 << uint(bits)

	occupancies := make([]Bitboard, size)
	attacks := make([]Bitboard, size)
	for i := 0; i < size; i++ {
		occ := subsetOf(i, mask)
		occupancies[i] = occ
		attacks[i] = slidingAttacksSlow(sq, occ, dirs)
	}

	table := make([]Bitboard, size)
	for attempt := 0; ; attempt++ {
		magic := sparseRandom(rng)
		// A magic multiplied by the mask should produce a number with many
		// high bits set -- a cheap pre-filter standard in every magic-finder,
		// since a candidate that fails this essentially never hashes well
		// anyway and it's far cheaper to check than a full fill attempt.
		if Bitboard(uint64(mask)*magic).PopCount() < 6 {
			continue
		}

		for i := range table {
			table[i] = 0
		}
		ok := true
		for i, occ := range occupancies {
			index := (uint64(occ) * magic) >> shift
			if table[index] != 0 && table[index] != attacks[i] {
				ok = false
				break
			}
			table[index] = attacks[i]
		}
		if ok {
			return magicEntry{mask: mask, magic: magic, shift: shift, table: table}
		}
	}
}

// subsetOf returns the i-th subset of mask's set bits, treating i's own bits
// as "which of mask's set bits (in increasing order) are included". Iterating
// i from 0 to 2^popcount(mask)-1 enumerates every subset of mask exactly
// once -- the standard technique for building a magic table's input space.
func subsetOf(i int, mask Bitboard) Bitboard {
	var result Bitboard
	m := mask
	bitIndex := 0
	for m.Any() {
		var sq Square
		sq, m = m.PopLSB()
		if i&(1<<uint(bitIndex)) != 0 {
			result = result.Set(sq)
		}
		bitIndex++
	}
	return result
}

// sparseRandom returns a random 64-bit value with relatively few bits set,
// which empirically finds valid magics far faster than uniform random
// 64-bit values (a well-known property in magic bitboard construction:
// ANDing a few random values concentrates the result's entropy into fewer
// bits, and sparse multipliers are more likely to produce the
// high-bit-heavy products a good magic needs).
func sparseRandom(rng *rand.Rand) uint64 {
	return rng.Uint64() & rng.Uint64() & rng.Uint64()
}

// RookAttacks returns a rook's attack set on sq given the full board
// occupancy (both colors, all pieces) -- the magic lookup itself: mask to
// the relevant occupancy, multiply, shift, index.
func RookAttacks(sq Square, occupancy Bitboard) Bitboard {
	e := &rookMagicTable[sq]
	index := (uint64(occupancy&e.mask) * e.magic) >> e.shift
	return e.table[index]
}

// BishopAttacks is RookAttacks' counterpart for the diagonal rays.
func BishopAttacks(sq Square, occupancy Bitboard) Bitboard {
	e := &bishopMagicTable[sq]
	index := (uint64(occupancy&e.mask) * e.magic) >> e.shift
	return e.table[index]
}

// QueenAttacks is the union of the rook and bishop rays -- a queen moves as
// either.
func QueenAttacks(sq Square, occupancy Bitboard) Bitboard {
	return RookAttacks(sq, occupancy) | BishopAttacks(sq, occupancy)
}
