package search

import (
	"math/rand"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

func TestBestMoveFindsMateInOne(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White}) // e1
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White}) // a1
	p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black}) // h8
	p.SetPiece(core.NewSquare(5, 6), core.Piece{Type: core.Pawn, Color: core.Black}) // f7
	p.SetPiece(core.NewSquare(6, 6), core.Piece{Type: core.Pawn, Color: core.Black}) // g7
	p.SetPiece(core.NewSquare(7, 6), core.Piece{Type: core.Pawn, Color: core.Black}) // h7
	ov := core.NewCardOverlay()

	s := NewSearcher()
	best, score, ok := s.BestMove(p, ov, Hands{}, core.White, true, 2)

	if !ok {
		t.Fatal("expected BestMove to find an action")
	}
	if best.Kind != actions.ActionMove || best.Move.To != core.NewSquare(0, 7) {
		t.Fatalf("expected Ra1-a8# (mate on the back rank), got %+v", best)
	}
	if score < scoreMate/2 {
		t.Errorf("expected a mate-scale score, got %d", score)
	}
}

func TestBestMoveFindsHangingCapture(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Queen, Color: core.Black}) // a8, undefended
	ov := core.NewCardOverlay()

	s := NewSearcher()
	best, _, ok := s.BestMove(p, ov, Hands{}, core.White, true, 2)

	if !ok {
		t.Fatal("expected BestMove to find an action")
	}
	if best.Kind != actions.ActionMove || best.Move.To != core.NewSquare(0, 7) {
		t.Fatalf("expected Rxa8 capturing the undefended queen, got %+v", best)
	}
}

// TestBestMoveCoordinatesFreezeThenCapture is the headline capability this
// whole rebuild targets: a card and a move combined beat either alone.
// White's rook can capture Black's rook, but it's defended once by a
// knight -- a direct capture is an even trade (R for R). Freezing the
// knight FIRST (same turn, per the turn model: card then move) removes the
// recapture entirely, netting a whole rook for free. The search must
// prefer the card action at the root over every plain move, including the
// direct capture.
func TestBestMoveCoordinatesFreezeThenCapture(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.King, Color: core.White})   // a1
	p.SetPiece(core.NewSquare(3, 0), core.Piece{Type: core.Rook, Color: core.White})   // d1
	p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black})   // h8
	p.SetPiece(core.NewSquare(3, 7), core.Piece{Type: core.Rook, Color: core.Black})   // d8
	p.SetPiece(core.NewSquare(1, 6), core.Piece{Type: core.Knight, Color: core.Black}) // b7, defends d8
	ov := core.NewCardOverlay()

	hands := Hands{White: actions.Hand{{ID: "c1", Mechanic: actions.MechanicFreeze}}}

	s := NewSearcher()
	best, _, ok := s.BestMove(p, ov, hands, core.White, true, 2)

	if !ok {
		t.Fatal("expected BestMove to find an action")
	}
	if best.Kind != actions.ActionCard || best.Card.Mechanic != actions.MechanicFreeze {
		t.Fatalf("expected the engine to play Freeze on the defending knight before capturing, got %+v", best)
	}
	if best.Targets.First != core.NewSquare(1, 6) {
		t.Fatalf("expected Freeze targeted at b7 (the defending knight), got %v", best.Targets.First)
	}
}

func TestNegamaxIsScoreConsistentAcrossPerspectives(t *testing.T) {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	s := NewSearcher()

	_, whiteScore, _ := s.BestMove(p, ov, Hands{}, core.White, true, 2)
	s2 := NewSearcher()
	_, blackScore, _ := s2.BestMove(p, ov, Hands{}, core.Black, true, 2)

	// The symmetric starting position should score close to 0 for either
	// side to move (not an exact equality -- search finds a real best line
	// for each side, and White-to-move vs Black-to-move aren't the same
	// search tree -- just a sanity bound against a wildly broken sign
	// convention, which would show as a huge, clearly-wrong magnitude).
	for _, sc := range []int{whiteScore, blackScore} {
		if sc > 300 || sc < -300 {
			t.Errorf("expected a roughly balanced score at the symmetric start, got %d", sc)
		}
	}
}

func TestFairPlaySearchReturnsSortedAggregatedResults(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Queen, Color: core.Black})
	ov := core.NewCardOverlay()
	rng := rand.New(rand.NewSource(1))

	results := FairPlaySearch(p, ov, nil, core.White, 3, 4, 2, rng)
	if len(results) == 0 {
		t.Fatal("expected at least one root action")
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Fatalf("expected results sorted descending by score, index %d (%v) > index %d (%v)", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
	top := results[0]
	if top.Action.Kind != actions.ActionMove || top.Action.Move.To != core.NewSquare(0, 7) {
		t.Errorf("expected the top-ranked action to still be capturing the hanging queen despite hidden-hand sampling, got %+v", top.Action)
	}
}

func TestFairPlaySearchTimedReturnsSortedAggregatedResults(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(4, 0), core.Piece{Type: core.King, Color: core.White})
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.Rook, Color: core.White})
	p.SetPiece(core.NewSquare(4, 7), core.Piece{Type: core.King, Color: core.Black})
	p.SetPiece(core.NewSquare(0, 7), core.Piece{Type: core.Queen, Color: core.Black})
	ov := core.NewCardOverlay()
	rng := rand.New(rand.NewSource(1))

	results := FairPlaySearchTimed(p, ov, nil, core.White, 3, 4, 200*time.Millisecond, 32, rng)
	if len(results) == 0 {
		t.Fatal("expected at least one root action")
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Fatalf("expected results sorted descending by score, index %d (%v) > index %d (%v)", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
	top := results[0]
	if top.Action.Kind != actions.ActionMove || top.Action.Move.To != core.NewSquare(0, 7) {
		t.Errorf("expected the top-ranked action to still be capturing the hanging queen despite hidden-hand sampling and a time budget, got %+v", top.Action)
	}
}

// TestBestMoveReportsNoActionWhenEveryMobilePieceIsFrozen is a permanent
// regression test for a real corruption bug: BestMove used to return a
// zero-valued actions.Action{} (Move{From:a1,To:a1}, a degenerate
// same-square "move") whenever GenerateActions found nothing, and every
// caller trusted it blindly. core.Position.movePiece's `from.Bit() |
// to.Bit()` mask collapses to a SINGLE bit when From==To, so the XOR
// toggles that bit OFF instead of leaving the piece in place -- silently
// deleting whatever piece sat on a1 from the board. Caught by self-play
// hitting exactly this position shape ~40 plies into a real game: every
// mobile piece frozen, nothing submittable, but not checkmate/stalemate
// (TerminalStatus is deliberately Frozen-blind, see actions/terminal.go).
// selfplay.go's GenerateSelfPlayGame now checks BestMove's ok return and
// ends the game gracefully instead of applying the placeholder Action.
//
// This position: White's king on a1 is boxed in NOT by occupancy but by
// three Black knights covering all three of its escape squares (a2, b1,
// b2) without attacking a1 itself -- so the king has zero legal moves but
// isn't in check. White's only other piece, a knight on g5, has several
// real legal moves (so raw GenerateLegalMovesWithOverlay is non-empty --
// not stalemate) but is Frozen, so GenerateSubmittableMoves filters it
// out -- leaving GenerateActions with literally nothing for White's
// (cardless) turn.
func TestBestMoveReportsNoActionWhenEveryMobilePieceIsFrozen(t *testing.T) {
	p := core.NewEmptyPosition()
	p.SetPiece(core.NewSquare(0, 0), core.Piece{Type: core.King, Color: core.White}) // a1
	knight := core.NewSquare(6, 4)                                                   // g5
	p.SetPiece(knight, core.Piece{Type: core.Knight, Color: core.White})
	p.SetPiece(core.NewSquare(7, 7), core.Piece{Type: core.King, Color: core.Black})   // h8
	p.SetPiece(core.NewSquare(1, 3), core.Piece{Type: core.Knight, Color: core.Black}) // b4, covers a2
	p.SetPiece(core.NewSquare(3, 1), core.Piece{Type: core.Knight, Color: core.Black}) // d2, covers b1
	p.SetPiece(core.NewSquare(2, 3), core.Piece{Type: core.Knight, Color: core.Black}) // c4, covers b2
	ov := core.NewCardOverlay()
	ov.SetFrozen(knight, true)

	if status := actions.TerminalStatus(p, ov, nil); status != core.Ongoing {
		t.Fatalf("expected Ongoing (Frozen-blind: the knight's raw moves keep this from being stalemate), got %v", status)
	}
	if len(core.GenerateSubmittableMoves(p, ov)) != 0 {
		t.Fatal("expected zero submittable moves: the king is boxed in and the only mobile piece is frozen")
	}

	posBefore := *p
	s := NewSearcher()
	best, _, ok := s.BestMove(p, ov, Hands{}, core.White, true, 2)

	if ok {
		t.Fatalf("expected BestMove to report no action available, got ok=true best=%+v", best)
	}
	if *p != posBefore {
		t.Fatal("BestMove must not mutate the position when it finds no action")
	}
}

func TestTranspositionTableExactBoundMatchesFreshSearch(t *testing.T) {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()

	fresh := NewSearcher()
	_, freshScore, _ := fresh.BestMove(p, ov, Hands{}, core.White, true, 2)

	// Warm start: reuse the same Searcher (and its populated TT) for a
	// second, identical search -- if the TT's bound classification were
	// wrong (the exact bug the plan flags in the old engine), a stale
	// cutoff could return a different, wrong answer here.
	_, warmScore, _ := fresh.BestMove(p, ov, Hands{}, core.White, true, 2)

	if freshScore != warmScore {
		t.Errorf("expected the TT-warmed search to reach the identical score, got %d fresh vs %d warm", freshScore, warmScore)
	}
}
