package search

import (
	"math/rand"
	"testing"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/engine/nnue"
)

func TestNNUEBackedSearcherFindsHangingCapture(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Queen, Color: core.Black})
	ov := core.NewCardOverlay()

	// An untrained (random-weight) network won't reliably play well, so
	// this only checks the WIRING: NNUEEvaluator drives a real search to a
	// real, legal decision without crashing -- training/strength is
	// Task 9/10's job, not this test's.
	net := nnue.NewRandomNetwork(rand.New(rand.NewSource(3)))
	s := NewSearcherWithEval(NNUEEvaluator(net))

	best, _ := s.BestMove(p, ov, Hands{}, core.White, 2)
	if best.Kind != actions.ActionMove || (best.Move.From != core.NewSquare(0, 0) && best.Move.From != core.NewSquare(4, 0)) {
		t.Fatalf("expected a move from White's rook or king (the only pieces on the board), got %+v", best)
	}
	if s.Nodes() == 0 {
		t.Fatal("expected the NNUE-backed search to actually visit nodes")
	}
}
