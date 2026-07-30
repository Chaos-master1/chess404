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
// blackhole). Returns a closure that undoes ALL of it: core's own
// incremental undo for the position, plus a full CardOverlay snapshot
// restore for everything else, since none of Thaw/ExpireShields/
// TickFortresses/ResolveLava/ResolveBombs/TickBlackHoles has an
// incremental undo of its own (they were built for one-shot application in
// Task 19's overlay work, not search backtracking). This is a deliberate
// simplicity/performance tradeoff for Phase 2's MVP -- CardOverlay.Clone()
// is cheap when its zone lists are short, which is the common case, but a
// real search hot path would eventually want incremental undo here too.
//
// The returned closure is the ONLY way to reference the position's own
// undo token: core.MakeMoveWithOverlay returns an unexported type that
// cannot be named (or stored in a struct field) from this package, only
// captured by a closure defined in the same call -- exactly what this
// function does.
func applyMoveWithTicks(p *core.Position, ov *core.CardOverlay, mover core.Color, m core.Move) func() {
	ovSnapshot := ov.Clone()
	u := core.MakeMoveWithOverlay(p, ov, m)

	core.ResolveLava(p, ov, m.To)
	core.ThawAfterMove(p, ov, mover)
	ov.ExpireShields(p.FullMoveNumber())
	ov.TickFortresses(mover)
	core.ResolveBombs(p, ov)
	core.TickBlackHoles(p, ov, mover)

	return func() {
		p.UnmakeMove(u)
		*ov = *ovSnapshot
	}
}
