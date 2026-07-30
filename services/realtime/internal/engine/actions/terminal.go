package actions

import "github.com/chess404/realtime/internal/engine/core"

// GameStatus is an alias for core.GameStatus -- Ongoing/Checkmate/Stalemate
// mean the same thing at this layer; TerminalStatus below adds exactly one
// more rule on top of core.TerminalStatusWithOverlay (hand-based
// suppression).
type GameStatus = core.GameStatus

// TerminalStatus classifies (p, ov, hand) for the side to move, matching
// internal/match's evaluateAutomaticMatchFinish specifically for the
// checkmate/stalemate part (match_cards.go:1980-2019). Fifty-move-rule,
// threefold repetition, and insufficient material need full game history
// (a position alone can't answer "has this occurred three times before" or
// "how many reversible plies in a row") and are left to engine/search's
// repetition table / draw detection, not this single-position query.
//
// core.TerminalStatusWithOverlay already gives the Frozen-blind chess
// verdict a real legality check would give (see overlays_movegen.go), but
// internal/match goes one step further: it suppresses that verdict
// entirely -- treats the game as still core.Ongoing -- whenever the side to
// move holds ANY card at all (match_cards.go:1998-2007), regardless of
// whether that specific card could realistically change the outcome. This
// is a real, deliberate-looking reference behavior (closer to "you're never
// truly out of options while you hold a card" than an obvious bug), and a
// conformant engine reproduces it rather than "fixing" it -- exactly the
// same posture Task 19 took with Frozen-blind stalemate detection.
func TerminalStatus(p *core.Position, ov *core.CardOverlay, hand Hand) GameStatus {
	mover := p.SideToMove()
	if p.KingSquare(mover) == core.NoSquare {
		// The mover's king isn't on the board at all. In ordinary chess
		// this never happens (checkmate always ends the game first), but
		// it IS reachable here: a card action can open a brand-new
		// attacking line onto the enemy king mid-turn (e.g. Fusion
		// collapsing a bishop+rook into a queen with a fresh diagonal),
		// and the subsequent move within that same turn can then capture
		// it before any end-of-turn terminal check would have caught it as
		// checkmate. internal/match's own movegen doesn't specially
		// exclude the enemy king as a capture target either (chess.go's
		// canTarget checks color only), so this is a latent possibility
		// there too, just vanishingly rare. Treated as the decisive loss it
		// obviously is, rather than letting a missing king reach
		// core.KingSquare's documented "caller bug" contract and panic
		// deep in the attack tables (core.NoSquare fed to a leaper-attack
		// table lookup).
		return core.Checkmate
	}
	status := core.TerminalStatusWithOverlay(p, ov)
	if status != core.Ongoing && hand.HasAnyCard() {
		return core.Ongoing
	}
	return status
}
