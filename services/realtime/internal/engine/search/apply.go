// Package search is Phase 2's L2: alpha-beta over the combined action
// space engine/actions defines, with cards as first-class tree nodes
// (the plan's headline capability -- "Freeze the defender, then Bxh7
// mates" as one coordinated search, not two independent decisions) and
// fair-play PIMC sampling for the opponent's hidden hand.
//
// Scope note (matching engine/actions' own): this searches over the seven
// mechanics engine/actions models as Actions, plus plain chess. It is a
// correct, working alpha-beta with the essentials (iterative deepening,
// a transposition table with CORRECT exact/lower/upper bound handling --
// the plan explicitly flags the old engine's TT never storing an exact
// score as a real bug, fixed here from the start rather than inherited --
// and simple capture-first move ordering), not the old engine's full
// technique checklist (PVS, aspiration windows, null-move pruning, late
// move reductions, futility/razoring, IID, SEE, killer/history/counter-move
// tables, Lazy SMP). The plan's Phase 2 gate is "solves the card-tactics
// suite; beats the Phase-0 engine decisively in the gauntlet" -- both are
// reachable with a correct, well-ordered alpha-beta at modest depth, and
// further search techniques are refinement, not a blocker, once this is
// demonstrably working and tested.
package search

import (
	"github.com/chess404/realtime/internal/engine/core"
)

// applyMoveWithTicks applies a chess move AND every turn-ending overlay
// effect internal/match's applyMove tail runs after a move commits (Frozen
// thaw, Shield expiry, Fortress/BlackHole ticks, Lava trigger, Bomb
// tick/explode -- Fog is skipped, matching engine/core's own scope), in the
// same relative order internal/match uses (match_actions.go: Lava right
// after the move, then the cleanup batch: thaw+shield, fortress, bomb,
// blackhole). Returns a closure that undoes all of it.
//
// Undo is a full VALUE snapshot of *p (Position has no pointers/slices --
// just fixed arrays and scalars -- so *p is already a complete, cheap deep
// copy) plus a CardOverlay.Clone() snapshot, restored wholesale, rather
// than core.MakeMoveWithOverlay's own incremental undo token. This is
// deliberate, not just simpler: ResolveLava/ResolveBombs/TickBlackHoles can
// remove pieces ANYWHERE on the board as a side effect of a trap/bomb/
// blackhole detonating -- not just at the square this move touched -- and
// core's own per-move undo token only ever reverses the ONE move it was
// created for. A real bug caught by TestDiagnosticUnabomberThroughRealNegamax
// (kept as a permanent regression test, see selfplay_test.go): White bombs
// its own pawn (2-turn fuse), moves it forward; while exploring ONE of
// Black's candidate replies a few plies later, the fuse reaches zero and
// detonates, deleting White's pawn from a completely different square than
// the move being explored touched. Undoing that reply via only its own
// move's incremental token left the pawn permanently missing for every
// OTHER sibling branch the search still had to explore -- corruption that
// surfaces later, arbitrarily far from its actual cause, exactly the kind
// of bug incremental per-move undo cannot repair on its own. A full
// Position snapshot/restore sidesteps the problem entirely: whatever
// changed, wherever it changed, gets put back.
func applyMoveWithTicks(p *core.Position, ov *core.CardOverlay, mover core.Color, m core.Move) func() {
	posSnapshot := *p
	ovSnapshot := ov.Clone()

	core.MakeMoveWithOverlay(p, ov, m)
	core.ResolveLava(p, ov, m.To)
	core.ThawAfterMove(p, ov, mover)
	ov.ExpireShields(p.FullMoveNumber())
	ov.TickFortresses(mover)
	core.ResolveBombs(p, ov)
	core.TickBlackHoles(p, ov, mover)

	return func() {
		*p = posSnapshot
		*ov = *ovSnapshot
	}
}
