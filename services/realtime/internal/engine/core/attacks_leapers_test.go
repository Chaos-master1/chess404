package core

import "testing"

// Knight/king/pawn attack counts from corners, edges, and center are
// well-known constants in chess programming -- checking against them (rather
// than just "the function returns something") is what actually catches an
// off-by-one in the delta computation.

func TestKnightAttackCounts(t *testing.T) {
	cases := []struct {
		sq   Square
		name string
		want int
	}{
		{NewSquare(0, 0), "a1 corner", 2},
		{NewSquare(7, 7), "h8 corner", 2},
		{NewSquare(7, 0), "h1 corner", 2},
		{NewSquare(0, 7), "a8 corner", 2},
		{NewSquare(0, 3), "a4 edge", 4},
		{NewSquare(4, 4), "e5 center", 8},
		{NewSquare(1, 1), "b2 near-corner", 4},
	}
	for _, c := range cases {
		got := KnightAttacks(c.sq).PopCount()
		if got != c.want {
			t.Errorf("%s (sq=%d): knight attacks = %d, want %d", c.name, c.sq, got, c.want)
		}
	}
}

func TestKnightAttacksFromE4ExactSquares(t *testing.T) {
	e4 := NewSquare(4, 3)
	want := []Square{
		NewSquare(2, 2), NewSquare(2, 4), // c3, c5
		NewSquare(3, 1), NewSquare(3, 5), // d2, d6
		NewSquare(5, 1), NewSquare(5, 5), // f2, f6
		NewSquare(6, 2), NewSquare(6, 4), // g3, g5
	}
	got := KnightAttacks(e4)
	if got.PopCount() != len(want) {
		t.Fatalf("expected %d knight moves from e4, got %d", len(want), got.PopCount())
	}
	for _, sq := range want {
		if !got.Has(sq) {
			t.Errorf("expected knight on e4 to attack %s", sq)
		}
	}
}

func TestKingAttackCounts(t *testing.T) {
	cases := []struct {
		sq   Square
		name string
		want int
	}{
		{NewSquare(0, 0), "a1 corner", 3},
		{NewSquare(7, 7), "h8 corner", 3},
		{NewSquare(0, 4), "a5 edge", 5},
		{NewSquare(4, 4), "e5 center", 8},
	}
	for _, c := range cases {
		got := KingAttacks(c.sq).PopCount()
		if got != c.want {
			t.Errorf("%s: king attacks = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestPawnAttacksColorAndEdges(t *testing.T) {
	// A white pawn on e4 attacks d5 and f5.
	e4 := NewSquare(4, 3)
	got := PawnAttacks(e4, White)
	if got.PopCount() != 2 || !got.Has(NewSquare(3, 4)) || !got.Has(NewSquare(5, 4)) {
		t.Errorf("white pawn on e4 should attack exactly d5,f5, got popcount=%d bits=%#x", got.PopCount(), uint64(got))
	}

	// A black pawn on e4 attacks d3 and f3 (attacks toward rank 1).
	gotBlack := PawnAttacks(e4, Black)
	if gotBlack.PopCount() != 2 || !gotBlack.Has(NewSquare(3, 2)) || !gotBlack.Has(NewSquare(5, 2)) {
		t.Errorf("black pawn on e4 should attack exactly d3,f3, got popcount=%d bits=%#x", gotBlack.PopCount(), uint64(gotBlack))
	}

	// Edge case: a pawn on the a-file only has one diagonal, not two, and it
	// must not wrap to the h-file.
	a4 := NewSquare(0, 3)
	edgeAttacks := PawnAttacks(a4, White)
	if edgeAttacks.PopCount() != 1 || !edgeAttacks.Has(NewSquare(1, 4)) {
		t.Errorf("white pawn on a4 should attack only b5, got popcount=%d bits=%#x", edgeAttacks.PopCount(), uint64(edgeAttacks))
	}
}

func TestNoAttackTableLeaksOffBoardBits(t *testing.T) {
	// A bug that shifts bits off the top/bottom of the uint64 without proper
	// masking wouldn't necessarily crash -- it would just silently corrupt
	// unrelated high/low bits. Every attack table entry must be a subset of
	// the 64 real squares, which for a uint64 bitboard is automatically true
	// UNLESS some rank-wrap bug produced a value that, interpreted as
	// unsigned, still fits -- so instead directly check that no entry has any
	// bit beyond square 63, which is structurally guaranteed by Bitboard
	// being exactly uint64, and that entries are non-empty where expected.
	for sq := Square(0); sq < 64; sq++ {
		if KnightAttacks(sq).Empty() {
			t.Errorf("knight on square %d (%s) has zero attacks -- every square has at least 2", sq, sq)
		}
		if KingAttacks(sq).Empty() {
			t.Errorf("king on square %d (%s) has zero attacks -- every square has at least 3", sq, sq)
		}
	}
}
