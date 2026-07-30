package core

import "testing"

func TestFENRoundTrip(t *testing.T) {
	cases := []string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w Qkq - 0 1",
		"rnbqkbnr/ppp1pppp/8/3pP3/8/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 3",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/8/8/8/8/8/8/4K2k w - - 5 40",
	}
	for _, fen := range cases {
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		got := p.ToFEN()
		if got != fen {
			t.Errorf("round trip mismatch:\n  original: %s\n  ToFEN():  %s", fen, got)
		}

		// Re-parsing the exported FEN must produce the identical position
		// (not just an identical string) -- the property that actually
		// matters for self-play export/import.
		reparsed, err := ParseFEN(got)
		if err != nil {
			t.Fatalf("re-parsing exported FEN %q: %v", got, err)
		}
		if p.Hash() != reparsed.Hash() {
			t.Errorf("re-parsed position's Hash() differs from the original for FEN %q", fen)
		}
	}
}
