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
	status := core.TerminalStatusWithOverlay(p, ov)
	if status != core.Ongoing && hand.HasAnyCard() {
		return core.Ongoing
	}
	return status
}
