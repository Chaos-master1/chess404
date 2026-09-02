package match

import (
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Automatic match finish evaluation, insufficient material, finish marking, temporary-effect cleanup.

func finalizeAutomaticMatchFinish(state *contracts.MatchState, events []contracts.ResolvedEvent, now time.Time, actorID string) []contracts.ResolvedEvent {
	if state == nil || state.Status != "active" {
		return events
	}

	winner, finishReason := evaluateAutomaticMatchFinish(state)
	if finishReason == "" {
		return events
	}

	markMatchFinished(state, winner, finishReason, now)

	clockUpdated := false
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type != "clock_updated" {
			continue
		}
		if events[index].Payload == nil {
			events[index].Payload = map[string]any{}
		}
		events[index].Payload["runningFor"] = ""
		clockUpdated = true
		break
	}
	if !clockUpdated {
		events = append(events, makeEvent(state.MatchID, "clock_updated", now, actorID, map[string]any{
			"runningFor": "",
		}))
	}

	payload := map[string]any{
		"result": finishReason,
	}
	if winner != "" {
		payload["winner"] = winner
	}
	events = append(events, makeEvent(state.MatchID, "match_finished", now, actorID, payload))
	return events
}

func shouldEvaluateAutomaticMatchFinish(state *contracts.MatchState, intent contracts.PlayerIntent) bool {
	if state == nil || state.Status != "active" {
		return false
	}
	if state.DoubleMove != nil && state.DoubleMove.MovesLeft > 0 {
		return false
	}

	switch intent.Type {
	case "make_move":
		return true
	case "play_card", "select_target":
		return state.PendingCard == nil
	default:
		return false
	}
}

func evaluateAutomaticMatchFinish(state *contracts.MatchState) (string, string) {
	if state == nil {
		return "", ""
	}

	if state.HalfMoveClock >= 100 {
		return "draw", "fifty_move_rule"
	}

	historyKeys := make([]string, 0, len(state.History))
	for _, position := range state.History {
		historyKeys = append(historyKeys, positionKey(position.Board, position.Turn, sliceToSet(position.Moved), position.LastMove, position.WhiteHand, position.BlackHand))
	}
	currentKey := positionKey(state.Board, state.Turn, sliceToSet(state.Moved), state.LastMove, state.WhiteHand, state.BlackHand)
	if threefold(historyKeys, currentKey) {
		return "draw", "threefold_repetition"
	}

	_, isMate, isStale := gameStatusWithFusion(state.Board, state.Turn, state.LastMove, sliceToSet(state.Moved), state.FortressZones)
	if isMate || isStale {
		hand := state.WhiteHand
		if state.Turn == "black" {
			hand = state.BlackHand
		}
		if len(hand) > 0 {
			return "", ""
		}
	}
	if isMate {
		return opposite(state.Turn), "checkmate"
	}
	if isStale {
		return "draw", "stalemate"
	}
	if insufficientMaterialForState(state) {
		return "draw", "insufficient_material"
	}

	return "", ""
}

func insufficientMaterialForState(state *contracts.MatchState) bool {
	if state.InvisiblePiece == nil {
		return insufficientMaterial(state.Board)
	}
	board := cloneBoard(state.Board)
	invisible := state.InvisiblePiece
	if inBounds(invisible.Row, invisible.Col) {
		piece := invisible.Piece
		board[invisible.Row][invisible.Col] = &piece
	}
	return insufficientMaterial(board)
}

func markMatchFinished(state *contracts.MatchState, winner string, finishReason string, now time.Time) {
	if state == nil {
		return
	}
	state.Status = "finished"
	state.Winner = winner
	state.FinishReason = finishReason
	state.DrawOfferedBy = ""
	state.Clock.RunningFor = ""
	state.Clock.StartedAt = nil
	state.PendingCard = nil
	state.DoubleMove = nil
	state.InvisiblePiece = nil
	state.FogZones = nil
	state.FortressZones = nil
	state.BombPieces = nil
	state.LavaSquares = nil
	state.BlackHoles = nil
	state.UndoAgainst = ""
	state.RadarRevealFor = ""
	state.CheaterState = nil
	state.UpdatedAt = now.UTC()
}

func cleanupTemporaryEffects(state *contracts.MatchState, justMovedColor string) {
	for r := 0; r < len(state.Board); r++ {
		for c := 0; c < len(state.Board[r]); c++ {
			piece := state.Board[r][c]
			if piece == nil {
				continue
			}
			if piece.Frozen && piece.Color == justMovedColor {
				piece.Frozen = false
			}
			if piece.Shielded && piece.ShieldTurn != nil && state.FullMoveNum >= *piece.ShieldTurn {
				piece.Shielded = false
				piece.ShieldTurn = nil
			}
			// Borrow returns the piece to its owner after one turn, but only if the
			// piece still exists. If the borrowed piece was destroyed by Death (or
			// any other removal effect) while under the borrower's control, it is
			// permanently lost — the owner does not get it back. This is intentional
			// emergent gameplay: Borrow gives tempo at the risk of losing the piece.
			if piece.Borrowed && piece.Color == justMovedColor {
				if state.Board[r][c] != nil {
					piece.Color = opposite(justMovedColor)
					piece.Borrowed = false
				}
			}
		}
	}
	if state.InvisiblePiece != nil {
		if state.InvisiblePiece.OwnerColor == justMovedColor {
			state.InvisiblePiece.RoundsLeft--
		} else if state.InvisiblePiece.RoundsLeft <= 0 {
			state.InvisiblePiece = nil
		}
	}
	if state.RadarRevealFor != "" && state.Turn != state.RadarRevealFor {
		state.RadarRevealFor = ""
	}
	if state.CheaterState != nil && state.Turn != state.CheaterState.OwnerColor {
		state.CheaterState.TurnsLeft--
		if state.CheaterState.TurnsLeft <= 0 {
			state.CheaterState = nil
		}
	}
	if state.UndoAgainst == justMovedColor {
		state.UndoAgainst = ""
	}
}
