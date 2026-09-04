package match

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Mirror, gambler, hand management, lava/bomb/fog/fortress/black-hole/parasite effect resolution.

func invalidateCastlingRightsForSquare(state *contracts.MatchState, sq contracts.Square) {
	key := keyForSquare(sq)
	for _, existing := range state.Moved {
		if existing == key {
			return
		}
	}
	switch key {
	case "0-4", "0-0", "0-7", "7-4", "7-0", "7-7":
		state.Moved = append(state.Moved, key)
	}
}

func resolveGamblerCard(state *contracts.MatchState, owner string, now time.Time) map[string]any {
	opponent := opposite(owner)
	var myHand, oppHand []contracts.GameCard
	if owner == "black" {
		myHand = state.BlackHand
		oppHand = state.WhiteHand
	} else {
		myHand = state.WhiteHand
		oppHand = state.BlackHand
	}

	roll := deterministicCardIndex(state, len(myHand)+len(oppHand)+int(state.RNGSeed%100))
	win := len(oppHand) > 0 && (roll%2 == 0 || len(myHand) <= 1)
	if win && len(oppHand) > 0 {
		stolenIndex := deterministicCardIndex(state, len(oppHand)+1) % len(oppHand)
		stolen := oppHand[stolenIndex]
		removeCardFromHand(state, opponent, stolen.ID)
		forceAddCardToHand(state, owner, stolen)
		return map[string]any{
			"outcome": "win",
			"card":    stolen,
		}
	}

	candidates := filterCardsNotMechanic(myHand, "gambler")
	if len(candidates) > 0 {
		giveIndex := deterministicCardIndex(state, len(candidates)+3) % len(candidates)
		given := candidates[giveIndex]
		removeCardFromHand(state, owner, given.ID)
		forceAddCardToHand(state, opponent, given)
		return map[string]any{
			"outcome": "lose",
			"card":    given,
		}
	}

	return map[string]any{
		"outcome": "none",
	}
}

// forceAddCardToHand always adds card, bypassing the maxHandSize check in addCardToHand
// to ensure the swap completes. The caller should enforce maxHandSize separately.
func forceAddCardToHand(state *contracts.MatchState, owner string, card contracts.GameCard) {
	if owner == "black" {
		state.BlackHand = append(state.BlackHand, card)
		return
	}
	state.WhiteHand = append(state.WhiteHand, card)
}

func resolveLavaEffects(state *contracts.MatchState, landing contracts.Square) (bool, string) {
	if len(state.LavaSquares) == 0 {
		return false, ""
	}

	triggered := false
	capturedPieceType := ""
	nextLava := make([]contracts.LavaSquare, 0, len(state.LavaSquares))
	for _, lava := range state.LavaSquares {
		if lava.Row == landing.Row && lava.Col == landing.Col {
			triggered = true
			piece := pieceAt(state.Board, landing)
			if piece != nil && piece.Type != "king" {
				if piece.Shielded {
					piece.Shielded = false
					piece.ShieldTurn = nil
				} else {
					capturedPieceType = piece.Type
					state.Board[landing.Row][landing.Col] = nil
				}
			}
			continue
		}

		lava.MovesLeft--
		if lava.MovesLeft > 0 {
			nextLava = append(nextLava, lava)
		}
	}
	state.LavaSquares = nextLava
	return triggered, capturedPieceType
}

func updateBombTracker(state *contracts.MatchState, from, to contracts.Square) {
	if len(state.BombPieces) == 0 {
		return
	}
	for i := range state.BombPieces {
		bomb := &state.BombPieces[i]
		if bomb.Row == from.Row && bomb.Col == from.Col {
			bomb.Row = to.Row
			bomb.Col = to.Col
			return
		}
	}
}

func resolveBombEffects(state *contracts.MatchState) []contracts.Square {
	if len(state.BombPieces) == 0 {
		return nil
	}

	nextBombs := make([]contracts.BombPiece, 0, len(state.BombPieces))
	exploded := make([]contracts.Square, 0)
	for _, bomb := range state.BombPieces {
		piece := pieceAt(state.Board, contracts.Square{Row: bomb.Row, Col: bomb.Col})
		if piece == nil || !piece.Bomb {
			continue
		}
		if piece.Color != bomb.OwnerColor {
			log.Printf("bomb dropped for match %s: bomb.OwnerColor=%s piece.Color=%s", state.MatchID, bomb.OwnerColor, piece.Color)
			continue
		}

		bomb.TurnsLeft--
		if bomb.TurnsLeft <= 0 {
			piece.Bomb = false
			for dr := -1; dr <= 1; dr++ {
				for dc := -1; dc <= 1; dc++ {
					r := bomb.Row + dr
					c := bomb.Col + dc
					if !inBounds(r, c) {
						continue
					}
					target := state.Board[r][c]
					if target != nil && target.Type != "king" {
						if target.Shielded {
							target.Shielded = false
							target.ShieldTurn = nil
							continue
						}
						state.Board[r][c] = nil
						exploded = append(exploded, contracts.Square{Row: r, Col: c})
					}
				}
			}
			continue
		}

		nextBombs = append(nextBombs, bomb)
	}

	state.BombPieces = nextBombs
	return exploded
}

func resolveFogEffects(state *contracts.MatchState, justMovedColor string) {
	if len(state.FogZones) == 0 {
		return
	}

	nextFog := make([]contracts.FogZone, 0, len(state.FogZones))
	for _, zone := range state.FogZones {
		if zone.OwnerColor != justMovedColor {
			zone.TurnsLeft--
		}
		if zone.TurnsLeft > 0 {
			nextFog = append(nextFog, zone)
		}
	}
	state.FogZones = nextFog
}

func resolveFortressEffects(state *contracts.MatchState, justMovedColor string) {
	if len(state.FortressZones) == 0 {
		return
	}

	nextFortress := make([]contracts.FortressZone, 0, len(state.FortressZones))
	for _, zone := range state.FortressZones {
		if zone.OwnerColor != justMovedColor {
			zone.TurnsLeft--
		}
		if zone.TurnsLeft > 0 {
			nextFortress = append(nextFortress, zone)
		}
	}
	state.FortressZones = nextFortress
}

func fortressEntryBlocked(zones []contracts.FortressZone, moverColor string, target contracts.Square) bool {
	for _, zone := range zones {
		if zone.OwnerColor == moverColor {
			continue
		}
		if target.Row >= zone.TopRow && target.Row <= zone.TopRow+1 && target.Col >= zone.LeftCol && target.Col <= zone.LeftCol+1 {
			return true
		}
	}
	return false
}

func resolveBlackHoleEffects(state *contracts.MatchState, justMovedColor string) []contracts.Square {
	if len(state.BlackHoles) == 0 {
		return nil
	}

	nextBlackHoles := make([]contracts.BlackHoleZone, 0, len(state.BlackHoles))
	exploded := make([]contracts.Square, 0)
	seen := make(map[string]struct{})

	blow := func(center contracts.Square) {
		for dr := -1; dr <= 1; dr++ {
			for dc := -1; dc <= 1; dc++ {
				r := center.Row + dr
				c := center.Col + dc
				if !inBounds(r, c) {
					continue
				}
				target := state.Board[r][c]
				if target != nil && target.Type != "king" {
					if target.Shielded {
						target.Shielded = false
						target.ShieldTurn = nil
						continue
					}
					state.Board[r][c] = nil
					key := keyForCoords(r, c)
					if _, ok := seen[key]; !ok {
						seen[key] = struct{}{}
						exploded = append(exploded, contracts.Square{Row: r, Col: c})
					}
				}
			}
		}
	}

	for _, hole := range state.BlackHoles {
		if hole.OwnerColor != justMovedColor {
			hole.TurnsLeft--
		}
		if hole.TurnsLeft <= 0 {
			blow(hole.Sq1)
			blow(hole.Sq2)
			continue
		}
		nextBlackHoles = append(nextBlackHoles, hole)
	}

	state.BlackHoles = nextBlackHoles
	return exploded
}

func resolveParasiteEffects(board [][]*contracts.Piece, from, to, capturedSquare contracts.Square, capturedPiece *contracts.Piece, fortressZones []contracts.FortressZone) error {
	if capturedPiece == nil {
		return nil
	}

	// Fake pieces are not real; skip parasite-linked destruction
	if capturedPiece.Fake {
		return nil
	}

	if capturedPiece.ParasiteTarget != "" {
		if hostSq, ok := parseParasiteSquare(capturedPiece.ParasiteTarget); ok {
			hostPiece := pieceAt(board, hostSq)
			if hostPiece != nil && hostPiece.Type != "king" {
				if hostPiece.Shielded {
					hostPiece.Shielded = false
					hostPiece.ShieldTurn = nil
				} else {
					if err := ensurePieceRemovalKeepsOwnKingSafe(board, hostSq, fortressZones); err != nil {
						return errors.New("parasite capture would leave a king in check")
					}
					board[hostSq.Row][hostSq.Col] = nil
				}
			}
		}
	}

	for r := 0; r < len(board); r++ {
		for c := 0; c < len(board[r]); c++ {
			piece := board[r][c]
			if piece == nil || piece.ParasiteTarget == "" || piece.Fake {
				continue
			}
			targetSq, ok := parseParasiteSquare(piece.ParasiteTarget)
			if !ok || targetSq.Row != capturedSquare.Row || targetSq.Col != capturedSquare.Col {
				continue
			}
			if piece.Type != "king" {
				if piece.Shielded {
					piece.Shielded = false
					piece.ShieldTurn = nil
					continue
				}
				if err := ensurePieceRemovalKeepsOwnKingSafe(board, contracts.Square{Row: r, Col: c}, fortressZones); err != nil {
					return errors.New("parasite capture would leave a king in check")
				}
				board[r][c] = nil
			}
		}
	}

	return nil
}

func updateParasiteLinksForMove(board [][]*contracts.Piece, from, to contracts.Square) {
	for r := 0; r < len(board); r++ {
		for c := 0; c < len(board[r]); c++ {
			piece := board[r][c]
			if piece == nil || piece.ParasiteTarget == "" {
				continue
			}
			targetSq, ok := parseParasiteSquare(piece.ParasiteTarget)
			if !ok || targetSq.Row != from.Row || targetSq.Col != from.Col {
				continue
			}
			piece.ParasiteTarget = fmt.Sprintf("%d,%d", to.Row, to.Col)
		}
	}
}

func parseParasiteSquare(value string) (contracts.Square, bool) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return contracts.Square{}, false
	}
	row, err := strconv.Atoi(parts[0])
	if err != nil {
		return contracts.Square{}, false
	}
	col, err := strconv.Atoi(parts[1])
	if err != nil {
		return contracts.Square{}, false
	}
	if !inBounds(row, col) {
		return contracts.Square{}, false
	}
	return contracts.Square{Row: row, Col: col}, true
}
