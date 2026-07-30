package search

import (
	"testing"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// TestBombDetonationDuringSiblingExplorationDoesNotCorruptPosition is a
// permanent regression test for a real bug: ResolveBombs/ResolveLava/
// TickBlackHoles can remove a piece from ANY square on the board as a side
// effect of a trap/bomb/blackhole detonating, not just the square the
// current move touched. applyMoveWithTicks used to undo only via core's
// own per-move incremental token, which has no way to know about (or
// reverse) a piece removed somewhere else entirely as a side effect of the
// SAME move's ticks. Concretely: White bombs its own pawn (2-turn fuse),
// then moves that pawn forward. A few plies later, while the search is
// exploring one of Black's candidate replies, the fuse reaches zero and
// detonates, deleting White's pawn from a square that reply's own move
// never touched. Undoing that reply via only its own incremental token
// left the pawn permanently missing on the position the search's search
// tree still holds in hand for every OTHER sibling branch -- corruption
// that surfaces arbitrarily far from its actual cause (in the original
// crash, as an index-out-of-range panic inside a completely unrelated
// move's UnmakeMove, several stack frames away). Fixed by snapshotting and
// restoring the whole Position by value (apply.go), not just one move's
// token.
func TestBombDetonationDuringSiblingExplorationDoesNotCorruptPosition(t *testing.T) {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	hands := Hands{White: actions.Hand{{ID: "c1", Mechanic: actions.MechanicUnabomber}}}

	bombOnOwnPawn := actions.Action{
		Kind:    actions.ActionCard,
		Card:    hands.White[0],
		Targets: actions.CardTargets{NumTargets: 1, First: core.NewSquare(1, 1)}, // b2
	}

	s := NewSearcherWithEval(defaultEvaluator)
	// depth=2 is enough to reach: White's card -> White's move (ticks the
	// bomb once) -> Black's reply (ticks it again, detonating it) -- the
	// exact depth the original crash reproduced at.
	score := s.applyAndRecurse(p, ov, hands, core.White, bombOnOwnPawn, true, 2, 1, -scoreInfinity, scoreInfinity)

	if s.Nodes() == 0 {
		t.Fatal("expected the search to have actually visited nodes")
	}
	_ = score // the exact score isn't the point; not panicking is
}
