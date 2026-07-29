package core

import "testing"

// Reference perft node counts from chessprogramming.org/Perft_Results --
// independently computed, widely cross-validated against many real engines,
// and the standard correctness oracle for a chess move generator. Matching
// these is much stronger evidence than "the tests I wrote pass": these
// numbers weren't written with this implementation in mind, so they can't
// share a blind spot with it the way a self-authored test suite might.
func TestPerftStartingPosition(t *testing.T) {
	want := []uint64{1, 20, 400, 8902, 197281, 4865609}
	p := NewStartingPosition()
	for depth, w := range want {
		if depth == 0 {
			continue
		}
		p2 := NewStartingPosition() // fresh position per depth: perft must not depend on leftover state from a shorter run
		got := Perft(p2, depth)
		if got != w {
			t.Errorf("startpos depth %d: got %d, want %d", depth, got, w)
		}
	}
	_ = p
}

// TestPerftKiwipete is the single most famous perft stress position,
// specifically constructed to exercise castling (both sides, both colors),
// en passant, and promotion all at once, plus deep tactical lines. Chosen
// because a movegen that only gets simple positions right but has a subtle
// castling-rights or en-passant bug will fail here even when it passes the
// starting position cleanly.
func TestPerftKiwipete(t *testing.T) {
	const fen = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"
	want := []uint64{1, 48, 2039, 97862, 4085603}
	for depth, w := range want {
		if depth == 0 {
			continue
		}
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("failed to parse Kiwipete FEN: %v", err)
		}
		got := Perft(p, depth)
		if got != w {
			t.Errorf("kiwipete depth %d: got %d, want %d", depth, got, w)
		}
	}
}

func TestPerftPosition3(t *testing.T) {
	// A famously tricky pure-endgame position for en-passant and check
	// evasion, with no castling rights to complicate things -- isolates
	// those two mechanics from castling bugs.
	const fen = "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1"
	want := []uint64{1, 14, 191, 2812, 43238, 674624}
	for depth, w := range want {
		if depth == 0 {
			continue
		}
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("failed to parse position 3 FEN: %v", err)
		}
		got := Perft(p, depth)
		if got != w {
			t.Errorf("position 3 depth %d: got %d, want %d", depth, got, w)
		}
	}
}

func TestPerftPosition4(t *testing.T) {
	// Exercises promotion-with-check and a queenside castle with the king
	// starting adjacent to an enemy-controlled square.
	const fen = "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1"
	want := []uint64{1, 6, 264, 9467, 422333}
	for depth, w := range want {
		if depth == 0 {
			continue
		}
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("failed to parse position 4 FEN: %v", err)
		}
		got := Perft(p, depth)
		if got != w {
			t.Errorf("position 4 depth %d: got %d, want %d", depth, got, w)
		}
	}
}

func TestPerftPosition5(t *testing.T) {
	const fen = "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8"
	want := []uint64{1, 44, 1486, 62379, 2103487}
	for depth, w := range want {
		if depth == 0 {
			continue
		}
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("failed to parse position 5 FEN: %v", err)
		}
		got := Perft(p, depth)
		if got != w {
			t.Errorf("position 5 depth %d: got %d, want %d", depth, got, w)
		}
	}
}

// TestPerftStartingPositionDeep6 is separated from TestPerftStartingPosition
// so `go test -short` can skip the genuinely expensive one (119M nodes) while
// everything else in this file still runs -- depth 5 (4.9M nodes) is already
// strong evidence; depth 6 is the belt-and-suspenders version.
func TestPerftStartingPositionDeep6(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping depth-6 perft (119M nodes) in -short mode")
	}
	p := NewStartingPosition()
	const want = 119060324
	got := Perft(p, 6)
	if got != want {
		t.Errorf("startpos depth 6: got %d, want %d", got, want)
	}
}
