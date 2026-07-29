package core

import "testing"

func TestSquareFileRank(t *testing.T) {
	cases := []struct {
		sq         Square
		file, rank int
		str        string
	}{
		{0, 0, 0, "a1"},
		{7, 7, 0, "h1"},
		{8, 0, 1, "a2"},
		{63, 7, 7, "h8"},
		{28, 4, 3, "e4"}, // e4: file e=4, rank 4 -> 0-based rank 3
	}
	for _, c := range cases {
		if got := c.sq.File(); got != c.file {
			t.Errorf("Square(%d).File() = %d, want %d", c.sq, got, c.file)
		}
		if got := c.sq.Rank(); got != c.rank {
			t.Errorf("Square(%d).Rank() = %d, want %d", c.sq, got, c.rank)
		}
		if got := c.sq.String(); got != c.str {
			t.Errorf("Square(%d).String() = %q, want %q", c.sq, got, c.str)
		}
		if got := NewSquare(c.file, c.rank); got != c.sq {
			t.Errorf("NewSquare(%d,%d) = %d, want %d", c.file, c.rank, got, c.sq)
		}
	}
}

func TestSquareRoundTripsWithContractsRowCol(t *testing.T) {
	// The whole point of this convention: converting a wire-format
	// contracts.Square{Row, Col} is exactly Row*8+Col, no remapping. Pin it
	// with the two convention-defining corners plus the center.
	type rc struct{ row, col int }
	cases := map[rc]Square{
		{0, 0}: 0,  // a1: Row 0 = rank 1 (this package's rank 0), Col 0 = file a
		{7, 7}: 63, // h8
		{3, 4}: 28, // e4
	}
	for pos, want := range cases {
		got := NewSquare(pos.col, pos.row) // NewSquare(file, rank) == NewSquare(col, row)
		if got != want {
			t.Errorf("Row=%d,Col=%d -> Square %d, want %d", pos.row, pos.col, got, want)
		}
	}
}

func TestBitboardSetClearHas(t *testing.T) {
	var b Bitboard
	if b.Has(5) {
		t.Fatal("empty bitboard should have no squares set")
	}
	b = b.Set(5)
	if !b.Has(5) {
		t.Fatal("expected square 5 to be set")
	}
	if b.Has(6) {
		t.Fatal("square 6 should not be set")
	}
	b = b.Clear(5)
	if b.Has(5) || b.Any() {
		t.Fatal("expected bitboard to be empty after clearing its only bit")
	}
}

func TestPopCountAndLSB(t *testing.T) {
	b := Square(0).Bit() | Square(10).Bit() | Square(63).Bit()
	if got := b.PopCount(); got != 3 {
		t.Fatalf("PopCount() = %d, want 3", got)
	}
	if got := b.LSB(); got != 0 {
		t.Fatalf("LSB() = %d, want 0", got)
	}

	var empty Bitboard
	if got := empty.LSB(); got != NoSquare {
		t.Fatalf("LSB() of empty board = %d, want NoSquare", got)
	}
}

func TestPopLSBIteratesAllSetBitsExactlyOnce(t *testing.T) {
	want := map[Square]bool{2: true, 15: true, 40: true, 63: true}
	var b Bitboard
	for sq := range want {
		b = b.Set(sq)
	}

	seen := map[Square]bool{}
	for b.Any() {
		var sq Square
		sq, b = b.PopLSB()
		if seen[sq] {
			t.Fatalf("square %d visited twice", sq)
		}
		seen[sq] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("visited %d squares, want %d", len(seen), len(want))
	}
	for sq := range want {
		if !seen[sq] {
			t.Fatalf("square %d was never visited", sq)
		}
	}
	if b != 0 {
		t.Fatalf("expected the bitboard to be fully drained, got %#x", uint64(b))
	}
}

func TestFileAndRankMasks(t *testing.T) {
	if got := FileMask(0); got != FileA {
		t.Errorf("FileMask(0) = %#x, want FileA = %#x", uint64(got), uint64(FileA))
	}
	if got := FileMask(7); got != FileH {
		t.Errorf("FileMask(7) = %#x, want FileH = %#x", uint64(got), uint64(FileH))
	}
	if got := RankMask(0); got != Rank1 {
		t.Errorf("RankMask(0) = %#x, want Rank1 = %#x", uint64(got), uint64(Rank1))
	}
	if got := RankMask(7); got != Rank8 {
		t.Errorf("RankMask(7) = %#x, want Rank8 = %#x", uint64(got), uint64(Rank8))
	}
	// FileA must contain exactly a1, a2, ..., a8 -- one bit per rank.
	if got := FileA.PopCount(); got != 8 {
		t.Errorf("FileA has %d bits set, want 8", got)
	}
	if !FileA.Has(NewSquare(0, 0)) || !FileA.Has(NewSquare(0, 7)) {
		t.Error("FileA should contain both a1 and a8")
	}
	if FileA.Has(NewSquare(1, 0)) {
		t.Error("FileA should not contain b1")
	}
}

func TestShiftsDoNotWrapAroundFiles(t *testing.T) {
	// The classic bug this masking exists to prevent: a piece on the h-file
	// "moving east" must vanish off the board, not reappear on the a-file of
	// the next rank.
	h4 := NewSquare(7, 3).Bit()
	if got := east(h4); got != 0 {
		t.Errorf("east() from the h-file must not wrap to the a-file, got %#x", uint64(got))
	}
	a4 := NewSquare(0, 3).Bit()
	if got := west(a4); got != 0 {
		t.Errorf("west() from the a-file must not wrap to the h-file, got %#x", uint64(got))
	}
	if got := northEast(h4); got != 0 {
		t.Errorf("northEast() from the h-file must not wrap, got %#x", uint64(got))
	}
	if got := southWest(a4); got != 0 {
		t.Errorf("southWest() from the a-file must not wrap, got %#x", uint64(got))
	}

	// A normal, non-edge shift should behave exactly like simple arithmetic.
	e4 := NewSquare(4, 3).Bit()
	wantNorth := NewSquare(4, 4).Bit()
	if got := north(e4); got != wantNorth {
		t.Errorf("north(e4) = %#x, want e5 = %#x", uint64(got), uint64(wantNorth))
	}
}
