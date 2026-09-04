package match

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// applySelectTarget: the pending-card target switch (freeze, bomb, transform, ...).

func applySelectTarget(state *contracts.MatchState, intent contracts.PlayerIntent, now time.Time) ([]contracts.ResolvedEvent, error) {
	if err := ensureActive(state); err != nil {
		return nil, err
	}
	if state.PendingCard == nil {
		return nil, errors.New("no pending card target selection")
	}
	pending := state.PendingCard
	owner, err := requireIntentColor(state, intent.PlayerID, intent.PlayerSecret)
	if err != nil {
		return nil, err
	}
	if owner != pending.OwnerColor {
		return nil, errors.New("only the card owner can select the target")
	}

	switch pending.Mechanic {
	case "freeze":
		if intent.Target == nil {
			return nil, errors.New("target selection requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil {
			return nil, errors.New("target square has no piece")
		}
		if targetPiece.Color == pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("freeze requires an enemy non-king target")
		}
		targetPiece.Frozen = true
		replaceLastHistorySnapshot(state)
	case "shield":
		if intent.Target == nil {
			return nil, errors.New("target selection requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil {
			return nil, errors.New("target square has no piece")
		}
		if targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("shield requires your own non-king target")
		}
		targetPiece.Shielded = true
		shieldTurn := state.FullMoveNum + 1
		targetPiece.ShieldTurn = &shieldTurn
		replaceLastHistorySnapshot(state)
	case "sniper":
		if intent.Target == nil {
			return nil, errors.New("target selection requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil {
			return nil, errors.New("target square has no piece")
		}
		if targetPiece.Color == pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("sniper requires an enemy non-king target")
		}
		if err := ensureRemovalDoesNotCreateCheck(state.Board, *intent.Target, pending.OwnerColor, state.FortressZones); err != nil {
			return nil, err
		}
		if targetPiece.Shielded {
			targetPiece.Shielded = false
			targetPiece.ShieldTurn = nil
			state.DrawOfferedBy = ""
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"effect": "shield_blocked_capture",
					"target": intent.Target,
				}),
			}, nil
		}
		state.Board[intent.Target.Row][intent.Target.Col] = nil
		replaceLastHistorySnapshot(state)
	case "badsniper":
		if intent.Target == nil {
			return nil, errors.New("target selection requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil {
			return nil, errors.New("target square has no piece")
		}
		if targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("badsniper requires your own non-king target")
		}
		if err := ensureRemovalDoesNotCreateCheck(state.Board, *intent.Target, pending.OwnerColor, state.FortressZones); err != nil {
			return nil, err
		}
		if targetPiece.Shielded {
			targetPiece.Shielded = false
			targetPiece.ShieldTurn = nil
			state.DrawOfferedBy = ""
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"effect": "shield_blocked_capture",
					"target": intent.Target,
				}),
			}, nil
		}
		state.Board[intent.Target.Row][intent.Target.Col] = nil
		replaceLastHistorySnapshot(state)
	case "promote", "demote", "promotehim", "demotehim":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("target selection requires a target square")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil {
				return nil, errors.New("target square has no piece")
			}
			if err := validateTransformTarget(targetPiece, pending.OwnerColor, pending.Mechanic, *intent.Target); err != nil {
				return nil, err
			}
			if targetPiece.Frozen && (pending.Mechanic == "demote" || pending.Mechanic == "demotehim" ||
				(pending.Mechanic == "promote" && targetPiece.Color == pending.OwnerColor) ||
				(pending.Mechanic == "promotehim" && targetPiece.Color != pending.OwnerColor)) {
				return nil, errors.New("transform cannot target a frozen piece")
			}
			options := safeTransformOptions(state.Board, *intent.Target, pending.Mechanic, state.FortressZones)
			if len(options) == 0 {
				return nil, fmt.Errorf("no safe %s options available", pending.Mechanic)
			}
			pending.Target = intent.Target
			pending.Options = options
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
					"options":  options,
				}),
			}, nil
		}

		if intent.SelectionID == "" {
			return nil, errors.New("transform selection requires a selectionId")
		}
		if !containsString(pending.Options, intent.SelectionID) {
			return nil, errors.New("selected transform is not allowed")
		}
		targetPiece := pieceAt(state.Board, *pending.Target)
		if targetPiece == nil {
			return nil, errors.New("pending target piece no longer exists")
		}
		targetPiece.Type = intent.SelectionID
		replaceLastHistorySnapshot(state)
	case "teleport":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("teleport requires selecting your piece first")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("teleport requires your own non-king target")
			}
			if targetPiece.Frozen {
				return nil, errors.New("teleport cannot target a frozen piece")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("teleport requires choosing a destination square")
		}
		if !inBounds(intent.Target.Row, intent.Target.Col) {
			return nil, errors.New("teleport destination is out of bounds")
		}
		if pieceAt(state.Board, *intent.Target) != nil {
			return nil, errors.New("teleport destination must be empty")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("teleport destination is protected by an enemy fortress")
		}
		movingPiece := pieceAt(state.Board, *pending.Target)
		if movingPiece == nil {
			return nil, errors.New("teleport source piece no longer exists")
		}
		if movingPiece.Frozen {
			return nil, errors.New("teleport cannot move a frozen piece")
		}
		nextBoard := cloneBoard(state.Board)
		nextMovingPiece := nextBoard[pending.Target.Row][pending.Target.Col]
		nextBoard[intent.Target.Row][intent.Target.Col] = nextMovingPiece
		nextBoard[pending.Target.Row][pending.Target.Col] = nil
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("teleport destination is not safe")
		}
		state.Board = nextBoard
		invalidateCastlingRightsForSquare(state, *pending.Target)
		replaceLastHistorySnapshot(state)
	case "jump":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("jump requires selecting your piece first")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" || targetPiece.Type == "knight" {
				return nil, errors.New("jump requires your own non-king, non-knight target")
			}
			if targetPiece.Frozen {
				return nil, errors.New("jump cannot target a frozen piece")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("jump requires choosing a destination square")
		}
		if !inBounds(intent.Target.Row, intent.Target.Col) {
			return nil, errors.New("jump destination is out of bounds")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("jump destination is protected by an enemy fortress")
		}
		fromPiece := pieceAt(state.Board, *pending.Target)
		if fromPiece == nil {
			return nil, errors.New("jump source piece no longer exists")
		}
		if fromPiece.Frozen {
			return nil, errors.New("jump cannot move a frozen piece")
		}
		destinationPiece := pieceAt(state.Board, *intent.Target)
		if destinationPiece != nil && destinationPiece.Color == pending.OwnerColor {
			return nil, errors.New("jump cannot land on your own piece")
		}
		if destinationPiece != nil && destinationPiece.Type == "king" {
			return nil, errors.New("jump cannot capture the king")
		}
		if !jumpDirectionValid(*pending.Target, *intent.Target, fromPiece.Type, fromPiece.Color) {
			return nil, errors.New("jump destination is invalid for that piece")
		}
		if !jumpHasExactlyOnePieceBetween(state.Board, *pending.Target, *intent.Target) {
			return nil, errors.New("jump must have exactly one piece in between")
		}
		if fromPiece.Type == "pawn" && pending.Target.Col == intent.Target.Col && destinationPiece != nil {
			return nil, errors.New("pawn can only jump straight to an empty square")
		}
		nextBoard := cloneBoard(state.Board)
		nextMovingPiece := nextBoard[pending.Target.Row][pending.Target.Col]
		nextBoard[intent.Target.Row][intent.Target.Col] = nextMovingPiece
		nextBoard[pending.Target.Row][pending.Target.Col] = nil
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("jump destination is not safe")
		}
		state.Board = nextBoard
		invalidateCastlingRightsForSquare(state, *pending.Target)
		replaceLastHistorySnapshot(state)
	case "swapme":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("swapme requires selecting your first piece")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("swapme requires your own non-king target")
			}
			if targetPiece.Frozen {
				return nil, errors.New("swapme cannot target a frozen piece")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("swapme requires selecting your second piece")
		}
		firstPiece := pieceAt(state.Board, *pending.Target)
		secondPiece := pieceAt(state.Board, *intent.Target)
		if firstPiece == nil {
			return nil, errors.New("swapme first piece no longer exists")
		}
		if secondPiece == nil || secondPiece.Color != pending.OwnerColor || secondPiece.Type == "king" {
			return nil, errors.New("swapme requires your own non-king second piece")
		}
		if secondPiece.Frozen {
			return nil, errors.New("swapme cannot target a frozen piece")
		}
		if pending.Target.Row == intent.Target.Row && pending.Target.Col == intent.Target.Col {
			return nil, errors.New("swapme requires two different pieces")
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[pending.Target.Row][pending.Target.Col], nextBoard[intent.Target.Row][intent.Target.Col] = nextBoard[intent.Target.Row][intent.Target.Col], nextBoard[pending.Target.Row][pending.Target.Col]
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("swapme would create check")
		}
		state.Board = nextBoard
		invalidateCastlingRightsForSquare(state, *pending.Target)
		invalidateCastlingRightsForSquare(state, *intent.Target)
		replaceLastHistorySnapshot(state)
	case "swapus":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("swapus requires selecting your piece first")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("swapus requires your own non-king target")
			}
			if targetPiece.Frozen {
				return nil, errors.New("swapus cannot target a frozen piece")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("swapus requires selecting an enemy piece")
		}
		firstPiece := pieceAt(state.Board, *pending.Target)
		secondPiece := pieceAt(state.Board, *intent.Target)
		if firstPiece == nil {
			return nil, errors.New("swapus first piece no longer exists")
		}
		if firstPiece.Frozen {
			return nil, errors.New("swapus cannot move a frozen piece")
		}
		if secondPiece == nil || secondPiece.Color == pending.OwnerColor || secondPiece.Type == "king" {
			return nil, errors.New("swapus requires an enemy non-king second piece")
		}
		if secondPiece.Frozen {
			return nil, errors.New("swapus cannot target a frozen piece")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("swapus cannot swap a piece into an enemy fortress")
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[pending.Target.Row][pending.Target.Col], nextBoard[intent.Target.Row][intent.Target.Col] = nextBoard[intent.Target.Row][intent.Target.Col], nextBoard[pending.Target.Row][pending.Target.Col]
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("swapus would create check")
		}
		state.Board = nextBoard
		invalidateCastlingRightsForSquare(state, *pending.Target)
		invalidateCastlingRightsForSquare(state, *intent.Target)
		replaceLastHistorySnapshot(state)
	case "swaphim":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("swaphim requires selecting the first enemy piece")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color == pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("swaphim requires an enemy non-king target")
			}
			if targetPiece.Frozen {
				return nil, errors.New("swaphim cannot target a frozen piece")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("swaphim requires selecting the second enemy piece")
		}
		firstPiece := pieceAt(state.Board, *pending.Target)
		secondPiece := pieceAt(state.Board, *intent.Target)
		if firstPiece == nil {
			return nil, errors.New("swaphim first piece no longer exists")
		}
		if secondPiece == nil || secondPiece.Color == pending.OwnerColor || secondPiece.Type == "king" {
			return nil, errors.New("swaphim requires an enemy non-king second piece")
		}
		if secondPiece.Frozen {
			return nil, errors.New("swaphim cannot target a frozen piece")
		}
		if pending.Target.Row == intent.Target.Row && pending.Target.Col == intent.Target.Col {
			return nil, errors.New("swaphim requires two different enemy pieces")
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[pending.Target.Row][pending.Target.Col], nextBoard[intent.Target.Row][intent.Target.Col] = nextBoard[intent.Target.Row][intent.Target.Col], nextBoard[pending.Target.Row][pending.Target.Col]
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("swaphim would create check")
		}
		state.Board = nextBoard
		invalidateCastlingRightsForSquare(state, *pending.Target)
		invalidateCastlingRightsForSquare(state, *intent.Target)
		replaceLastHistorySnapshot(state)
	case "borrow":
		if intent.Target == nil {
			return nil, errors.New("borrow requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil || targetPiece.Color == pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("borrow requires an enemy non-king target")
		}
		if targetPiece.Frozen {
			return nil, errors.New("borrow cannot target a frozen piece")
		}
		if targetPiece.BorrowCount >= 3 {
			return nil, errors.New("piece has been borrowed too many times")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("borrow cannot target a piece inside an enemy fortress")
		}
		nextBoard := cloneBoard(state.Board)
		nextTarget := nextBoard[intent.Target.Row][intent.Target.Col]
		nextTarget.Color = pending.OwnerColor
		nextTarget.Borrowed = true
		nextTarget.BorrowCount++
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("borrow target is not safe")
		}
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "mindcontrol":
		if intent.Target == nil {
			return nil, errors.New("mindcontrol requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil || targetPiece.Color == pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("mindcontrol requires an enemy non-king target")
		}
		if targetPiece.Frozen {
			return nil, errors.New("mindcontrol cannot target a frozen piece")
		}
		if targetPiece.Shielded {
			return nil, errors.New("mindcontrol cannot target a shielded piece")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("mindcontrol cannot target a piece inside an enemy fortress")
		}
		nextBoard := cloneBoard(state.Board)
		nextTarget := nextBoard[intent.Target.Row][intent.Target.Col]
		nextTarget.Color = pending.OwnerColor
		nextTarget.Borrowed = false
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("mindcontrol target is not safe")
		}
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "parasite":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("parasite requires selecting your host piece first")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("parasite requires your own non-king host")
			}
			pending.Target = intent.Target
			pending.Options = []string{strconv.Itoa(pieceValue(targetPiece.Type))}
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
					"options":  pending.Options,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("parasite requires selecting an enemy target")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil || targetPiece.Color == pending.OwnerColor || targetPiece.Type == "king" || targetPiece.Fake {
			return nil, errors.New("parasite requires an enemy non-king target")
		}
		hostPiece := pieceAt(state.Board, *pending.Target)
		if hostPiece == nil {
			return nil, errors.New("parasite host no longer exists")
		}
		if len(pending.Options) == 0 {
			return nil, errors.New("parasite host value is missing")
		}
		hostValue, err := strconv.Atoi(pending.Options[0])
		if err != nil {
			return nil, errors.New("parasite host value is invalid")
		}
		if pieceValue(targetPiece.Type) != hostValue {
			return nil, fmt.Errorf("parasite requires an enemy piece with the same value (%d)", hostValue)
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[pending.Target.Row][pending.Target.Col].ParasiteTarget = fmt.Sprintf("%d,%d", intent.Target.Row, intent.Target.Col)
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "clone":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("clone requires selecting your piece first")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("clone requires your own non-king target")
			}
			if targetPiece.Frozen {
				return nil, errors.New("clone cannot target a frozen piece")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("clone requires choosing a destination square")
		}
		if !inBounds(intent.Target.Row, intent.Target.Col) {
			return nil, errors.New("clone destination is out of bounds")
		}
		sourcePiece := pieceAt(state.Board, *pending.Target)
		if sourcePiece == nil {
			return nil, errors.New("clone source piece no longer exists")
		}
		if sourcePiece.Frozen {
			return nil, errors.New("clone cannot copy a frozen piece")
		}
		if pieceAt(state.Board, *intent.Target) != nil {
			return nil, errors.New("clone destination must be empty")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("clone destination is protected by an enemy fortress")
		}
		if abs(intent.Target.Row-pending.Target.Row) > 1 || abs(intent.Target.Col-pending.Target.Col) > 1 || (intent.Target.Row == pending.Target.Row && intent.Target.Col == pending.Target.Col) {
			return nil, errors.New("clone destination must be adjacent")
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[intent.Target.Row][intent.Target.Col] = &contracts.Piece{
			Type:          sourcePiece.Type,
			Color:         sourcePiece.Color,
			Shielded:      sourcePiece.Shielded,
			ShieldTurn:    sourcePiece.ShieldTurn,
			Frozen:        sourcePiece.Frozen,
			Borrowed:      sourcePiece.Borrowed,
			Bomb:          sourcePiece.Bomb,
			Invisible:     sourcePiece.Invisible,
			InvisibleTurn: sourcePiece.InvisibleTurn,
			InvisibleOver: sourcePiece.InvisibleOver,
			FusedWith:     sourcePiece.FusedWith,
		}
		if !kingsRemainSafe(nextBoard, state.FortressZones) {
			return nil, errors.New("clone destination is not safe")
		}
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "fakepiece":
		if intent.Target == nil {
			return nil, errors.New("fakepiece requires a target square")
		}
		if !inBounds(intent.Target.Row, intent.Target.Col) {
			return nil, errors.New("fakepiece target is out of bounds")
		}
		if pieceAt(state.Board, *intent.Target) != nil {
			return nil, errors.New("fakepiece must target an empty square")
		}
		if fortressEntryBlocked(state.FortressZones, pending.OwnerColor, *intent.Target) {
			return nil, errors.New("fake piece cannot be placed inside an enemy fortress")
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[intent.Target.Row][intent.Target.Col] = &contracts.Piece{
			Type:  "pawn",
			Color: pending.OwnerColor,
			Fake:  true,
		}
		king := findKing(nextBoard, pending.OwnerColor)
		if king != nil && isAttackedWithFusion(nextBoard, *king, opposite(pending.OwnerColor), state.FortressZones) {
			return nil, errors.New("placing fakepiece there would expose your king")
		}
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "blackhole":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("blackhole requires the first target square")
			}
			if intent.Target.Row < 0 || intent.Target.Row > 7 || intent.Target.Col < 0 || intent.Target.Col > 7 {
				return nil, errors.New("blackhole target out of bounds")
			}
			pending.Target = intent.Target
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("blackhole requires the second target square")
		}
		if intent.Target.Row < 0 || intent.Target.Row > 7 || intent.Target.Col < 0 || intent.Target.Col > 7 {
			return nil, errors.New("blackhole target out of bounds")
		}
		if pending.Target.Row == intent.Target.Row && pending.Target.Col == intent.Target.Col {
			return nil, errors.New("blackhole requires two different target squares")
		}
		state.BlackHoles = append(state.BlackHoles, contracts.BlackHoleZone{
			Sq1:        *pending.Target,
			Sq2:        *intent.Target,
			TurnsLeft:  2,
			OwnerColor: pending.OwnerColor,
		})
		replaceLastHistorySnapshot(state)
	case "smallsacrifice", "bigsacrifice":
		goal := 6
		rewardCount := 2
		if pending.Mechanic == "bigsacrifice" {
			goal = 14
			rewardCount = 3
		}
		selected := parseSquareOptions(pending.Options)
		if intent.Target == nil {
			return nil, errors.New("sacrifice requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil {
			totalValue := selectedSquaresValue(state.Board, selected)
			if totalValue < goal {
				return nil, fmt.Errorf("sacrifice requires at least %d points", goal)
			}
			nextBoard := cloneBoard(state.Board)
			for _, sq := range selected {
				nextBoard[sq.Row][sq.Col] = nil
			}
			king := findKing(nextBoard, pending.OwnerColor)
			if king != nil && isAttackedWithFusion(nextBoard, *king, opposite(pending.OwnerColor), state.FortressZones) {
				return nil, errors.New("cannot sacrifice because it would leave your king in check")
			}
			state.Board = nextBoard
			drawn := addRewardCards(state, pending.OwnerColor, rewardCount, now)
			replaceLastHistorySnapshot(state)
			removeCardFromHand(state, pending.OwnerColor, pending.CardID)
			state.PendingCard = nil
			state.DrawOfferedBy = ""
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":     pending.CardID,
					"mechanic":   pending.Mechanic,
					"target":     intent.Target,
					"selected":   selected,
					"totalValue": totalValue,
					"drawnCards": drawn,
				}),
			}, nil
		}
		if targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("sacrifice requires your own non-king pieces")
		}
		updated := toggleSquareInList(selected, *intent.Target)
		pending.Options = encodeSquareOptions(updated)
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
				"cardId":     pending.CardID,
				"mechanic":   pending.Mechanic,
				"target":     intent.Target,
				"selected":   updated,
				"totalValue": selectedSquaresValue(state.Board, updated),
			}),
		}, nil
	case "lavaground":
		if intent.Target == nil {
			return nil, errors.New("lavaground requires a target square")
		}
		if pieceAt(state.Board, *intent.Target) != nil {
			return nil, errors.New("lavaground must target an empty square")
		}
		for _, lava := range state.LavaSquares {
			if lava.Row == intent.Target.Row && lava.Col == intent.Target.Col {
				return nil, errors.New("lavaground already exists on that square")
			}
		}
		state.LavaSquares = append(state.LavaSquares, contracts.LavaSquare{
			Row:       intent.Target.Row,
			Col:       intent.Target.Col,
			MovesLeft: 2,
		})
		replaceLastHistorySnapshot(state)
	case "fog_village":
		if intent.Target == nil {
			return nil, errors.New("fog_village requires a target square")
		}
		centerRow := intent.Target.Row
		centerCol := intent.Target.Col
		if centerRow < 1 {
			centerRow = 1
		} else if centerRow > 6 {
			centerRow = 6
		}
		if centerCol < 1 {
			centerCol = 1
		} else if centerCol > 6 {
			centerCol = 6
		}
		nextFog := make([]contracts.FogZone, 0, len(state.FogZones)+1)
		for _, zone := range state.FogZones {
			if zone.OwnerColor != pending.OwnerColor {
				nextFog = append(nextFog, zone)
			}
		}
		nextFog = append(nextFog, contracts.FogZone{
			CenterRow:  centerRow,
			CenterCol:  centerCol,
			TurnsLeft:  2,
			OwnerColor: pending.OwnerColor,
		})
		state.FogZones = nextFog
		replaceLastHistorySnapshot(state)
	case "fortress":
		if intent.Target == nil {
			return nil, errors.New("fortress requires a target square")
		}
		topRow := clampInt(intent.Target.Row, 0, 6)
		leftCol := clampInt(intent.Target.Col, 0, 6)
		nextFortress := make([]contracts.FortressZone, 0, len(state.FortressZones)+1)
		for _, zone := range state.FortressZones {
			if zone.OwnerColor != pending.OwnerColor {
				nextFortress = append(nextFortress, zone)
			}
		}
		nextFortress = append(nextFortress, contracts.FortressZone{
			TopRow:     topRow,
			LeftCol:    leftCol,
			TurnsLeft:  2,
			OwnerColor: pending.OwnerColor,
		})
		state.FortressZones = nextFortress
		replaceLastHistorySnapshot(state)
	case "invisible":
		if intent.Target == nil {
			return nil, errors.New("invisible requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("invisible requires your own non-king target")
		}
		nextBoard := cloneBoard(state.Board)
		nextPiece := nextBoard[intent.Target.Row][intent.Target.Col]
		nextBoard[intent.Target.Row][intent.Target.Col] = nil
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
		inFog := false
		for _, fz := range state.FogZones {
			if abs(intent.Target.Row-fz.CenterRow) <= 1 && abs(intent.Target.Col-fz.CenterCol) <= 1 {
				inFog = true
				break
			}
		}
		state.InvisiblePiece = &contracts.InvisiblePieceState{
			Row:        intent.Target.Row,
			Col:        intent.Target.Col,
			Piece:      *nextPiece,
			OwnerColor: pending.OwnerColor,
			RoundsLeft: 1,
			InFogZone:  inFog,
		}
	case "unabomber":
		if intent.Target == nil {
			return nil, errors.New("unabomber requires a target square")
		}
		targetPiece := pieceAt(state.Board, *intent.Target)
		if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
			return nil, errors.New("unabomber requires your own non-king target")
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[intent.Target.Row][intent.Target.Col].Bomb = true
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
		state.BombPieces = append(state.BombPieces, contracts.BombPiece{
			Row:        intent.Target.Row,
			Col:        intent.Target.Col,
			TurnsLeft:  2,
			OwnerColor: pending.OwnerColor,
		})
	case "halffuse":
		const halfFuseCap = 6
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("halffuse requires selecting your first piece")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("halffuse requires your own non-king piece")
			}
			if targetPiece.FusedWith != "" {
				return nil, errors.New("halffuse cannot target an already fused piece")
			}
			value := pieceValue(targetPiece.Type)
			if value >= halfFuseCap {
				return nil, errors.New("halffuse first piece is too expensive")
			}
			pending.Target = intent.Target
			pending.Options = []string{targetPiece.Type, strconv.Itoa(value)}
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("halffuse requires selecting the second piece")
		}
		if pending.Target.Row == intent.Target.Row && pending.Target.Col == intent.Target.Col {
			return nil, errors.New("halffuse requires a different second piece")
		}
		if abs(intent.Target.Row-pending.Target.Row) > 1 || abs(intent.Target.Col-pending.Target.Col) > 1 {
			return nil, errors.New("halffuse requires adjacent pieces")
		}
		if len(pending.Options) < 2 {
			return nil, errors.New("halffuse first piece metadata is missing")
		}
		firstType := pending.Options[0]
		firstValue, err := strconv.Atoi(pending.Options[1])
		if err != nil {
			return nil, errors.New("halffuse first piece value is invalid")
		}
		secondPiece := pieceAt(state.Board, *intent.Target)
		if secondPiece == nil || secondPiece.Color != pending.OwnerColor || secondPiece.Type == "king" {
			return nil, errors.New("halffuse requires your own non-king second piece")
		}
		if secondPiece.FusedWith != "" {
			return nil, errors.New("halffuse cannot target an already fused second piece")
		}
		isBishopRook := (firstType == "bishop" && secondPiece.Type == "rook") || (firstType == "rook" && secondPiece.Type == "bishop")
		if !isBishopRook && firstValue+pieceValue(secondPiece.Type) > halfFuseCap {
			return nil, errors.New("halffuse combined value exceeds the cap")
		}
		if redundancy := fusionRedundancy(firstType, secondPiece.Type, *pending.Target, *intent.Target); redundancy != "" {
			return nil, errors.New(redundancy)
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[pending.Target.Row][pending.Target.Col] = nil
		targetOnNext := nextBoard[intent.Target.Row][intent.Target.Col]
		if targetOnNext == nil {
			return nil, errors.New("halffuse second piece no longer exists")
		}
		if isBishopRook {
			targetOnNext.Type = "queen"
			targetOnNext.FusedWith = ""
		} else {
			targetOnNext.FusedWith = firstType
		}
		if !kingsRemainSafeWithFusion(nextBoard, state.FortressZones) {
			return nil, errors.New("halffuse would leave a king in check")
		}
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "fullfusion":
		if pending.Target == nil {
			if intent.Target == nil {
				return nil, errors.New("fullfusion requires selecting your first piece")
			}
			targetPiece := pieceAt(state.Board, *intent.Target)
			if targetPiece == nil || targetPiece.Color != pending.OwnerColor || targetPiece.Type == "king" {
				return nil, errors.New("fullfusion requires your own non-king piece")
			}
			if targetPiece.FusedWith != "" {
				return nil, errors.New("fullfusion cannot target an already fused piece")
			}
			pending.Target = intent.Target
			pending.Options = []string{targetPiece.Type}
			state.UpdatedAt = now.UTC()
			return []contracts.ResolvedEvent{
				makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
					"cardId":   pending.CardID,
					"mechanic": pending.Mechanic,
					"target":   intent.Target,
				}),
			}, nil
		}
		if intent.Target == nil {
			return nil, errors.New("fullfusion requires selecting the second piece")
		}
		if pending.Target.Row == intent.Target.Row && pending.Target.Col == intent.Target.Col {
			return nil, errors.New("fullfusion requires a different second piece")
		}
		if abs(intent.Target.Row-pending.Target.Row) > 1 || abs(intent.Target.Col-pending.Target.Col) > 1 {
			return nil, errors.New("fullfusion requires adjacent pieces")
		}
		if len(pending.Options) < 1 {
			return nil, errors.New("fullfusion first piece metadata is missing")
		}
		firstType := pending.Options[0]
		secondPiece := pieceAt(state.Board, *intent.Target)
		if secondPiece == nil || secondPiece.Color != pending.OwnerColor || secondPiece.Type == "king" {
			return nil, errors.New("fullfusion requires your own non-king second piece")
		}
		if secondPiece.FusedWith != "" {
			return nil, errors.New("fullfusion cannot target an already fused second piece")
		}
		if redundancy := fusionRedundancy(firstType, secondPiece.Type, *pending.Target, *intent.Target); redundancy != "" {
			return nil, errors.New(redundancy)
		}
		nextBoard := cloneBoard(state.Board)
		nextBoard[pending.Target.Row][pending.Target.Col] = nil
		targetOnNext := nextBoard[intent.Target.Row][intent.Target.Col]
		if targetOnNext == nil {
			return nil, errors.New("fullfusion second piece no longer exists")
		}
		isBishopRook := (firstType == "bishop" && secondPiece.Type == "rook") || (firstType == "rook" && secondPiece.Type == "bishop")
		if isBishopRook {
			targetOnNext.Type = "queen"
			targetOnNext.FusedWith = ""
		} else {
			targetOnNext.FusedWith = firstType
		}
		if !kingsRemainSafeWithFusion(nextBoard, state.FortressZones) {
			return nil, errors.New("fullfusion would leave a king in check")
		}
		state.Board = nextBoard
		replaceLastHistorySnapshot(state)
	case "joker":
		if intent.SelectionID == "" {
			return nil, errors.New("joker transform requires a selectionId")
		}
		if !containsString(pending.Options, intent.SelectionID) {
			return nil, errors.New("selected joker transform is not allowed")
		}
		template, found := starterCardTemplate(intent.SelectionID)
		if !found {
			return nil, errors.New("selected joker transform template was not found")
		}
		removeCardFromHand(state, pending.OwnerColor, pending.CardID)
		template.ID = fmt.Sprintf("joker_%s_%s_%d", template.Mechanic, pending.OwnerColor, now.UnixMilli())
		addCardToHand(state, pending.OwnerColor, template)
		state.PendingCard = nil
		state.DrawOfferedBy = ""
		state.UpdatedAt = now.UTC()
		return []contracts.ResolvedEvent{
			makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, map[string]any{
				"cardId":      pending.CardID,
				"mechanic":    pending.Mechanic,
				"selectionId": intent.SelectionID,
				"newCard":     template,
			}),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported pending mechanic: %s", pending.Mechanic)
	}

	targetPayload := any(nil)
	if intent.Target != nil {
		targetPayload = intent.Target
	} else if pending.Target != nil {
		targetPayload = pending.Target
	}

	removeCardFromHand(state, pending.OwnerColor, pending.CardID)
	state.PendingCard = nil
	state.DrawOfferedBy = ""
	state.UpdatedAt = now.UTC()
	// Every mechanic reaching this shared tail already called
	// replaceLastHistorySnapshot mid-case, immediately after mutating the
	// board but BEFORE PendingCard/the hand are updated above -- so the
	// history slot for this turn was frozen with a STALE, still-"pending"
	// snapshot: PendingCard still set, the card still in hand. History is
	// what "reverse" restores two half-moves later (applyPlayCard's
	// "reverse" case, restorePositionState) -- restoring that stale
	// snapshot resurrects an already-fully-resolved card as if it were
	// freshly pending again, and since applyPlayCard rejects ANY play_card
	// while ANY PendingCard is set (regardless of whose), this silently
	// disables card play entirely, for either side, for the rest of the
	// match. Found by xgauntlet's E0 cross-engine gauntlet: a real game had
	// exactly this happen -- an "invisible" card that had already resolved
	// cleanly reappeared as pending five plies later, right after a
	// "reverse" card, with a select_target for it then rejected with "only
	// the card owner can select the target" because Turn had moved on to
	// the other color. Re-capturing here, after the tail's own mutations,
	// overwrites that stale snapshot with the fully-resolved one --
	// replaceLastHistorySnapshot replaces the same history slot in place,
	// so calling it again is safe and cheap, not a second push.
	replaceLastHistorySnapshot(state)

	payload := map[string]any{
		"cardId":   pending.CardID,
		"mechanic": pending.Mechanic,
		"target":   targetPayload,
	}
	if intent.SelectionID != "" {
		payload["selectionId"] = intent.SelectionID
	}

	return []contracts.ResolvedEvent{
		makeEvent(state.MatchID, "target_selected", now, intent.PlayerID, payload),
	}, nil
}

func applyMirrorCard(state *contracts.MatchState, owner string) (bool, *contracts.Square, *contracts.Square, error) {
	if state.LastMove == nil {
		return false, nil, nil, nil
	}

	movedPiece := pieceAt(state.Board, state.LastMove.To)
	if movedPiece == nil {
		return false, nil, nil, nil
	}

	dr := state.LastMove.To.Row - state.LastMove.From.Row
	dc := state.LastMove.To.Col - state.LastMove.From.Col
	movedType := movedPiece.Type

	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			piece := state.Board[row][col]
			if piece == nil || piece.Color != owner || piece.Type != movedType {
				continue
			}

			from := contracts.Square{Row: row, Col: col}
			to := contracts.Square{Row: row + dr, Col: col + dc}
			if !inBounds(to.Row, to.Col) {
				continue
			}
			if occupant := pieceAt(state.Board, to); occupant != nil && occupant.Color == owner {
				continue
			}

			capturedSquare := to
			captured := pieceAt(state.Board, to)
			if captured != nil && captured.Shielded {
				captured.Shielded = false
				captured.ShieldTurn = nil
				state.DrawOfferedBy = ""
				return true, &from, &to, nil
			}
			nextBoard := cloneBoard(state.Board)
			nextPiece := pieceAt(nextBoard, from)
			if nextPiece == nil {
				continue
			}

			movePiece(nextBoard, from, to, nextPiece, false)
			if err := resolveParasiteEffects(nextBoard, from, to, capturedSquare, captured, state.FortressZones); err != nil {
				continue
			}
			updateParasiteLinksForMove(nextBoard, from, to)

			king := findKing(nextBoard, owner)
			if king != nil && isAttackedWithFusion(nextBoard, *king, opposite(owner), state.FortressZones) {
				continue
			}

			state.Board = nextBoard
			updateBombTracker(state, from, to)
			resolveLavaEffects(state, to)
			replaceLastHistorySnapshot(state)
			return true, &from, &to, nil
		}
	}

	return false, nil, nil, nil
}
