package search

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

func TestBestMoveTimedAlwaysCompletesDepthOneRegardlessOfBudget(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	ov := core.NewCardOverlay()

	s := NewSearcher()
	best, _, depthReached, ok := s.BestMoveTimed(p, ov, Hands{}, core.White, true, 0, 32)

	if !ok {
		t.Fatal("expected a legal action even with a zero time budget")
	}
	if depthReached < 1 {
		t.Fatalf("expected depth 1 to always complete, got depthReached=%d", depthReached)
	}
	if best.Kind != actions.ActionMove {
		t.Fatalf("expected a move, got %+v", best)
	}
}

func TestBestMoveTimedReachesGreaterDepthWithMoreBudget(t *testing.T) {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()

	s := NewSearcher()
	_, _, depthReached, ok := s.BestMoveTimed(p, ov, Hands{}, core.White, true, 300*time.Millisecond, 32)

	if !ok {
		t.Fatal("expected BestMoveTimed to find an action from the starting position")
	}
	if depthReached < 2 {
		t.Fatalf("expected a 300ms budget to reach at least depth 2 from the starting position, got %d", depthReached)
	}
}

func TestBestMoveTimedStopsEarlyOnForcedMate(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White}) // e1
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White}) // a1
	p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black}) // h8
	p.SetPiece(core.NewSquare(5, 6), core.Piece{Type: core.Pawn, Color: core.Black}) // f7
	p.SetPiece(core.NewSquare(6, 6), core.Piece{Type: core.Pawn, Color: core.Black}) // g7
	p.SetPiece(core.NewSquare(7, 6), core.Piece{Type: core.Pawn, Color: core.Black}) // h7
	ov := core.NewCardOverlay()

	s := NewSearcher()
	best, score, depthReached, ok := s.BestMoveTimed(p, ov, Hands{}, core.White, true, 5*time.Second, 32)

	if !ok {
		t.Fatal("expected BestMoveTimed to find the mate")
	}
	if !isMateScore(score) {
		t.Fatalf("expected a mate-scale score, got %d", score)
	}
	if best.Kind != actions.ActionMove || best.Move.To != core.NewSquare(0, 7) {
		t.Fatalf("expected Ra1-a8# (mate on the back rank), got %+v", best)
	}
	// A generous 5s budget must not be fully consumed once mate is found at
	// a shallow depth -- confirms the early-stop actually fired rather
	// than deepening all the way to maxDepth regardless.
	if depthReached >= 32 {
		t.Fatalf("expected early stop well before maxDepth once a forced mate is found, got depthReached=%d", depthReached)
	}
}

func TestBestMoveTimedReportsNoActionWhenEveryMobilePieceIsFrozen(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.King, Color: core.White})
	knight := core.NewSquare(6, 4) // g5
	p.SetPiece(knight, core.Piece{Type: core.Knight, Color: core.White})
	p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black})
	p.SetPiece(core.NewSquare(1, 3), core.Piece{Type: core.Knight, Color: core.Black}) // b4, covers a2
	p.SetPiece(core.NewSquare(3, 1), core.Piece{Type: core.Knight, Color: core.Black}) // d2, covers b1
	p.SetPiece(core.NewSquare(2, 3), core.Piece{Type: core.Knight, Color: core.Black}) // c4, covers b2
	ov := core.NewCardOverlay()
	ov.SetFrozen(knight, true)

	s := NewSearcher()
	_, _, _, ok := s.BestMoveTimed(p, ov, Hands{}, core.White, true, 100*time.Millisecond, 32)

	if ok {
		t.Fatal("expected BestMoveTimed to report no action available, matching BestMove's own contract")
	}
}
