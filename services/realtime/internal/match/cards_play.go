package match

import (
	"errors"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// applyPlayCard entry point.

func applyPlayCard(state *contracts.MatchState, intent contracts.PlayerIntent, now time.Time) ([]contracts.ResolvedEvent, error) {
	if err := ensureActive(state); err != nil {
		return nil, err
	}

	owner, err := requireIntentColor(state, intent.PlayerID, intent.PlayerSecret)
	if err != nil {
		return nil, err
	}
	if owner != state.Turn {
		return nil, errors.New("cards can only be played on your turn")
	}
	if state.DoubleMove != nil {
		return nil, errors.New("resolve the active double move before playing another card")
	}
	if state.PendingCard != nil {
		return nil, errors.New("resolve the pending card target first")
	}

	card, found := cardFromHand(state, owner, intent.CardID)
	if !found {
		return nil, errors.New("card not found in hand")
	}
	if state.UndoAgainst == owner {
		removeCardFromHand(state, owner, card.ID)
		state.UndoAgainst = ""
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":    card.ID,
				"mechanic":  card.Mechanic,
				"nullified": true,
			}),
			makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
				"effect": "undo_nullified_card",
			}),
		}, nil
	}

	if card.Mechanic == "doublemove_diff" || card.Mechanic == "doublemove_same" {
		moveType := "diff"
		if card.Mechanic == "doublemove_same" {
			moveType = "same"
		}
		removeCardFromHand(state, owner, card.ID)
		state.DoubleMove = &contracts.DoubleMoveState{
			Type:      moveType,
			MovesLeft: 2,
		}
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":     card.ID,
				"mechanic":   card.Mechanic,
				"doubleMove": state.DoubleMove,
			}),
		}, nil
	}
	if card.Mechanic == "undo" {
		removeCardFromHand(state, owner, card.ID)
		state.UndoAgainst = opposite(owner)
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":      card.ID,
				"mechanic":    card.Mechanic,
				"undoAgainst": state.UndoAgainst,
			}),
		}, nil
	}
	if card.Mechanic == "reverse" {
		if len(state.History) < 2 {
			return nil, errors.New("no move to reverse yet")
		}
		restored := state.History[len(state.History)-2]
		if king := findKing(restored.Board, owner); king != nil && isAttackedWithFusion(restored.Board, *king, opposite(owner), state.FortressZones) {
			return nil, errors.New("cannot reverse because your king would be in check")
		}
		if oppKing := findKing(restored.Board, opposite(owner)); oppKing != nil && isAttackedWithFusion(restored.Board, *oppKing, owner, state.FortressZones) {
			return nil, errors.New("cannot reverse because enemy king would be in check")
		}
		restorePositionState(state, restored)
		removeCardFromHand(state, owner, card.ID)
		state.UpdatedAt = now.UTC()
		events := []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":   card.ID,
				"mechanic": card.Mechanic,
			}),
			makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
				"effect": "reverse_applied",
			}),
		}
		if winner, reason := evaluateAutomaticMatchFinish(state); reason != "" {
			markMatchFinished(state, winner, reason, now)
			events = append(events, makeEvent(state.MatchID, "match_finished", now, intent.PlayerID, map[string]any{
				"result": reason,
				"winner": winner,
			}))
		}
		return events, nil
	}
	if card.Mechanic == "mirror" {
		removeCardFromHand(state, owner, card.ID)
		mirrored, from, to, err := applyMirrorCard(state, owner)
		if err != nil {
			return nil, err
		}
		// guard: if DoubleMove is active, decrement MovesLeft
		if state.DoubleMove != nil {
			state.DoubleMove.MovesLeft--
		}
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()

		payload := map[string]any{
			"cardId":   card.ID,
			"mechanic": card.Mechanic,
			"mirrored": mirrored,
		}
		if from != nil {
			payload["from"] = from
		}
		if to != nil {
			payload["to"] = to
		}

		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, payload),
			makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, payload),
		}, nil
	}
	if card.Mechanic == "gambler" {
		removeCardFromHand(state, owner, card.ID)
		payload := resolveGamblerCard(state, owner, now)
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		payload["cardId"] = card.ID
		payload["mechanic"] = card.Mechanic
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, payload),
		}, nil
	}
	if card.Mechanic == "radar" {
		removeCardFromHand(state, owner, card.ID)
		state.RadarRevealFor = owner
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":         card.ID,
				"mechanic":       card.Mechanic,
				"radarRevealFor": owner,
			}),
		}, nil
	}
	if card.Mechanic == "cheater" {
		removeCardFromHand(state, owner, card.ID)
		state.CheaterState = &contracts.CheaterState{
			OwnerColor: owner,
			TurnsLeft:  3,
		}
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":       card.ID,
				"mechanic":     card.Mechanic,
				"cheaterState": state.CheaterState,
			}),
		}, nil
	}
	if card.Mechanic == "joker" {
		state.PendingCard = &contracts.PendingCardState{
			CardID:     card.ID,
			Mechanic:   card.Mechanic,
			OwnerColor: owner,
			Options:    jokerTransformOptions(),
		}
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
				"cardId":   card.ID,
				"mechanic": card.Mechanic,
				"options":  state.PendingCard.Options,
			}),
		}, nil
	}

	state.PendingCard = &contracts.PendingCardState{
		CardID:     card.ID,
		Mechanic:   card.Mechanic,
		OwnerColor: owner,
	}
	state.UpdatedAt = now.UTC()

	return []contracts.ResolvedEvent{
		makeEvent(state.MatchID, "card_played", now, intent.PlayerID, map[string]any{
			"cardId":   card.ID,
			"mechanic": card.Mechanic,
		}),
	}, nil
}
