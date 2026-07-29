package core

import (
	"math/rand"
	"testing"
)

// The property that actually matters for an incremental hash: at every
// point, Hash() must equal computeHash() run fresh against the same
// position. computeHash is the independent oracle (a plain scan, no
// incremental state to get out of sync) -- this is the same
// oracle-comparison strategy attacks_sliders_test.go uses for the magic
// tables, applied to the hash instead.
func assertHashMatchesRecompute(t *testing.T, p *Position, label string) {
	t.Helper()
	if got, want := p.Hash(), computeHash(p); got != want {
		t.Fatalf("%s: incremental Hash()=%#x does not match a full recompute=%#x", label, got, want)
	}
}

func TestHashMatchesRecomputeAtConstruction(t *testing.T) {
	assertHashMatchesRecompute(t, NewStartingPosition(), "starting position")
	assertHashMatchesRecompute(t, NewEmptyPosition(), "empty position")

	p, err := ParseFEN("r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	if err != nil {
		t.Fatalf("failed to parse FEN: %v", err)
	}
	assertHashMatchesRecompute(t, p, "kiwipete")
}

// TestHashMatchesRecomputeThroughoutRandomGame plays a long sequence of
// random legal moves (make AND unmake, exercising every move type perft
// already proved were legal) and checks the incremental/recompute property
// after every single one -- not just at the end. If any single
// MakeMove/UnmakeMove code path forgets to update part of the hash, this
// catches it at the exact ply it happens, across many different game
// shapes since it reruns with several seeds.
func TestHashMatchesRecomputeThroughoutRandomGame(t *testing.T) {
	for seed := int64(0); seed < 5; seed++ {
		rng := rand.New(rand.NewSource(seed))
		p := NewStartingPosition()
		var undos []undo
		var movesPlayed []Move

		for ply := 0; ply < 60; ply++ {
			moves := GenerateLegalMoves(p)
			if len(moves) == 0 {
				break
			}
			m := moves[rng.Intn(len(moves))]
			u := p.MakeMove(m)
			undos = append(undos, u)
			movesPlayed = append(movesPlayed, m)
			assertHashMatchesRecompute(t, p, "after move")
		}

		for i := len(undos) - 1; i >= 0; i-- {
			p.UnmakeMove(undos[i])
			assertHashMatchesRecompute(t, p, "after unmake")
		}
		assertHashMatchesRecompute(t, p, "back at start")
		if p.Hash() != NewStartingPosition().Hash() {
			t.Errorf("seed %d: hash after unwinding the whole game does not match a fresh starting position", seed)
		}
	}
}

func TestSamePositionByDifferentMoveOrdersHashesIdentically(t *testing.T) {
	// The property a transposition table actually depends on: two move
	// orders that genuinely reach the same position must hash identically.
	//
	// The textbook example (1.Nf3 d5 2.d4 vs 1.d4 d5 2.Nf3) is deliberately
	// NOT used here: it doesn't actually transpose under this package's (and
	// internal/match's -- see chess.go's FEN-string builder, which sets the
	// en-passant field from "was the last move a 2-square pawn move" alone,
	// with no check for an adjacent capturing pawn) en-passant convention.
	// Order A's last move is d4, a double push, so its final position has a
	// dangling en-passant square (d3) even though nothing can capture there.
	// Order B's last move is Nf3, a non-pawn move, which clears any pending
	// en-passant square. Same pieces, same squares, different en-passant
	// state -- genuinely different positions, correctly different hashes
	// (this is exactly what TestDifferentPositionsHashDifferently already
	// asserts for an en-passant-square difference). Using pure knight
	// development instead sidesteps en passant entirely, so the only thing
	// under test is transposition, not this separate (correct) behavior.
	a := NewStartingPosition()
	aMoves := []Move{
		{From: NewSquare(6, 0), To: NewSquare(5, 2)}, // Nf3
		{From: NewSquare(6, 7), To: NewSquare(5, 5)}, // Nf6
		{From: NewSquare(1, 0), To: NewSquare(2, 2)}, // Nc3
	}
	for _, m := range aMoves {
		a.MakeMove(m)
	}

	b := NewStartingPosition()
	bMoves := []Move{
		{From: NewSquare(1, 0), To: NewSquare(2, 2)}, // Nc3
		{From: NewSquare(6, 7), To: NewSquare(5, 5)}, // Nf6
		{From: NewSquare(6, 0), To: NewSquare(5, 2)}, // Nf3
	}
	for _, m := range bMoves {
		b.MakeMove(m)
	}

	if a.Hash() != b.Hash() {
		t.Errorf("transposed move orders should hash identically: %#x vs %#x", a.Hash(), b.Hash())
	}
	assertHashMatchesRecompute(t, a, "order A")
	assertHashMatchesRecompute(t, b, "order B")
}

func TestDifferentPositionsHashDifferently(t *testing.T) {
	// Not a rigorous collision-resistance proof (impossible to fully verify
	// for a 64-bit hash), but a basic sanity check that obviously-different
	// positions -- including ones differing only in castling rights or the
	// en-passant square, which the previous engine's hash omitted entirely
	// -- don't collide by construction.
	positions := []*Position{
		NewStartingPosition(),
		MustParseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w Qkq - 0 1"),  // one castling right gone
		MustParseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR b KQkq - 0 1"), // side to move differs
		MustParseFEN("rnbqkbnr/ppp1pppp/8/3pP3/8/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 3"), // en passant square set
	}
	seen := map[uint64]int{}
	for i, p := range positions {
		h := p.Hash()
		if prev, ok := seen[h]; ok {
			t.Errorf("position %d and %d hashed identically (%#x) despite being different positions", prev, i, h)
		}
		seen[h] = i
	}
}

func TestCastlingRightHashChangesWhenRightIsLost(t *testing.T) {
	p := NewEmptyPosition()
	p.SetPiece(NewSquare(4, 0), Piece{Type: King, Color: White})
	p.SetPiece(NewSquare(7, 0), Piece{Type: Rook, Color: White})
	p.SetPiece(NewSquare(0, 7), Piece{Type: King, Color: Black})
	p.castling = CastleWhiteKingside | CastleWhiteQueenside
	p.hash = computeHash(p)

	before := p.Hash()
	u := p.MakeMove(Move{From: NewSquare(7, 0), To: NewSquare(7, 3)}) // rook moves, losing kingside rights
	after := p.Hash()

	if before == after {
		t.Error("expected the hash to change when a castling right is lost")
	}
	assertHashMatchesRecompute(t, p, "after losing a castling right")

	p.UnmakeMove(u)
	if p.Hash() != before {
		t.Error("expected the hash to be restored after unmaking the rook move")
	}
}

func TestEnPassantHashChangesWithEnPassantSquare(t *testing.T) {
	p := NewStartingPosition()
	before := p.Hash()
	u := p.MakeMove(Move{From: NewSquare(4, 1), To: NewSquare(4, 3), Flag: DoublePawnPush}) // e4
	after := p.Hash()

	if before == after {
		t.Error("expected the hash to change when a double push opens an en passant square")
	}
	assertHashMatchesRecompute(t, p, "after a double push")

	p.UnmakeMove(u)
	if p.Hash() != before {
		t.Error("expected the hash to be restored after unmaking the double push")
	}
}
