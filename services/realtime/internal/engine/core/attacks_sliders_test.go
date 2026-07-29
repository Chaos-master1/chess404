package core

import (
	"math/rand"
	"testing"
)

// The magic bitboard tables are only as trustworthy as their agreement with
// the slow, obviously-correct ray tracer they were built from. This is an
// exhaustive check: every square, a large sample of occupancy patterns
// (including the two totally-unambiguous edge cases, empty and full board),
// magic lookup must equal the oracle exactly. This is the test that would
// catch a broken magic, a mask that's off by one square, or a shift that's
// one bit too wide/narrow -- all classic magic-bitboard bugs that otherwise
// only show up as a rare wrong move in an obscure middlegame position.

func TestRookAttacksMatchSlowOracleExhaustively(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for sq := Square(0); sq < 64; sq++ {
		if got, want := RookAttacks(sq, 0), rookAttacksSlow(sq, 0); got != want {
			t.Fatalf("sq=%s empty board: RookAttacks=%#x want=%#x", sq, uint64(got), uint64(want))
		}
		if got, want := RookAttacks(sq, AllSquares), rookAttacksSlow(sq, AllSquares); got != want {
			t.Fatalf("sq=%s full board: RookAttacks=%#x want=%#x", sq, uint64(got), uint64(want))
		}
		for i := 0; i < 200; i++ {
			occ := Bitboard(rng.Uint64())
			got := RookAttacks(sq, occ)
			want := rookAttacksSlow(sq, occ)
			if got != want {
				t.Fatalf("sq=%s occ=%#x: RookAttacks=%#x want=%#x", sq, uint64(occ), uint64(got), uint64(want))
			}
		}
	}
}

func TestBishopAttacksMatchSlowOracleExhaustively(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for sq := Square(0); sq < 64; sq++ {
		if got, want := BishopAttacks(sq, 0), bishopAttacksSlow(sq, 0); got != want {
			t.Fatalf("sq=%s empty board: BishopAttacks=%#x want=%#x", sq, uint64(got), uint64(want))
		}
		if got, want := BishopAttacks(sq, AllSquares), bishopAttacksSlow(sq, AllSquares); got != want {
			t.Fatalf("sq=%s full board: BishopAttacks=%#x want=%#x", sq, uint64(got), uint64(want))
		}
		for i := 0; i < 200; i++ {
			occ := Bitboard(rng.Uint64())
			got := BishopAttacks(sq, occ)
			want := bishopAttacksSlow(sq, occ)
			if got != want {
				t.Fatalf("sq=%s occ=%#x: BishopAttacks=%#x want=%#x", sq, uint64(occ), uint64(got), uint64(want))
			}
		}
	}
}

// TestSliderAttacksAgainstRealisticOccupancy specifically exercises
// occupancy patterns that look like real chess positions (sparse, ~20-32
// bits set) rather than uniform-random 64-bit values, which tend to be much
// denser than any real position ever is. Magic construction doesn't care
// about the difference, but this guards against a bug that only manifests
// at realistic densities.
func TestSliderAttacksAgainstRealisticOccupancy(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for trial := 0; trial < 500; trial++ {
		var occ Bitboard
		numPieces := 16 + rng.Intn(17) // 16..32, like a real game
		for occ.PopCount() < numPieces {
			occ = occ.Set(Square(rng.Intn(64)))
		}
		sq := Square(rng.Intn(64))
		if got, want := RookAttacks(sq, occ), rookAttacksSlow(sq, occ); got != want {
			t.Fatalf("realistic occupancy trial %d: sq=%s RookAttacks=%#x want=%#x", trial, sq, uint64(got), uint64(want))
		}
		if got, want := BishopAttacks(sq, occ), bishopAttacksSlow(sq, occ); got != want {
			t.Fatalf("realistic occupancy trial %d: sq=%s BishopAttacks=%#x want=%#x", trial, sq, uint64(got), uint64(want))
		}
	}
}

func TestKnownReferenceAttackCounts(t *testing.T) {
	// Rook on a1, empty board: the entire a-file above it (7) plus the
	// entire first rank beside it (7) = 14. A textbook reference value.
	a1 := NewSquare(0, 0)
	if got := RookAttacks(a1, 0).PopCount(); got != 14 {
		t.Errorf("rook on a1, empty board: %d attacks, want 14", got)
	}

	// Bishop on d4, empty board: 13, the maximum for any square (center
	// squares see all four diagonals run the longest).
	d4 := NewSquare(3, 3)
	if got := BishopAttacks(d4, 0).PopCount(); got != 13 {
		t.Errorf("bishop on d4, empty board: %d attacks, want 13", got)
	}

	// Bishop in a corner, empty board: only the one long diagonal = 7.
	if got := BishopAttacks(a1, 0).PopCount(); got != 7 {
		t.Errorf("bishop on a1, empty board: %d attacks, want 7", got)
	}

	// Rook on d4 boxed in on all four sides by adjacent occupied squares:
	// attacks exactly those 4 adjacent squares (each ray is blocked
	// immediately, capturing the blocker but reaching no further).
	boxed := d4.Bit()
	for _, s := range []Square{NewSquare(3, 4), NewSquare(3, 2), NewSquare(4, 3), NewSquare(2, 3)} {
		boxed = boxed.Set(s)
	}
	if got := RookAttacks(d4, boxed).PopCount(); got != 4 {
		t.Errorf("rook on d4 boxed in on all 4 sides: %d attacks, want 4", got)
	}

	// Queen = rook | bishop: on an empty d4, that's 13+14=27 squares (the
	// two sets never overlap, straight vs diagonal rays share only the
	// origin square, which is excluded from both).
	if got := QueenAttacks(d4, 0).PopCount(); got != 27 {
		t.Errorf("queen on d4, empty board: %d attacks, want 27", got)
	}
}

func TestRelevantMaskExcludesEdgeSquares(t *testing.T) {
	// A rook's relevant occupancy mask on a1 must exclude h1 and a8 (the far
	// edge squares) since a piece sitting exactly on the edge can't be
	// "jumped" -- the ray always stops there regardless. It must include
	// everything strictly between.
	a1 := NewSquare(0, 0)
	mask := relevantMask(a1, rookDirs[:])
	if mask.Has(NewSquare(7, 0)) {
		t.Error("rook relevant mask on a1 should exclude h1 (the far edge of the rank)")
	}
	if mask.Has(NewSquare(0, 7)) {
		t.Error("rook relevant mask on a1 should exclude a8 (the far edge of the file)")
	}
	if !mask.Has(NewSquare(3, 0)) || !mask.Has(NewSquare(0, 3)) {
		t.Error("rook relevant mask on a1 should include interior squares like d1 and a4")
	}
	// 6 interior file squares + 6 interior rank squares = 12, the textbook
	// maximum for a rook (corner squares have the maximum relevant-bit count
	// since neither ray is shortened by being near an edge on the OTHER axis).
	if got := mask.PopCount(); got != 12 {
		t.Errorf("rook relevant mask on a1: %d bits, want 12", got)
	}
}
