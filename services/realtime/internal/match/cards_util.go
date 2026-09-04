package match

import (
	"errors"

	"github.com/chess404/realtime/internal/contracts"
)

// Piece values, clamps, board-safety invariants for transforms/removals, fusion redundancy, jump validation.

func pieceValue(pieceType string) int {
	switch pieceType {
	case "pawn":
		return 1
	case "knight", "bishop":
		return 3
	case "rook":
		return 5
	case "queen":
		return 9
	default:
		return 0
	}
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func ensurePieceRemovalKeepsOwnKingSafe(board [][]*contracts.Piece, square contracts.Square, fortressZones []contracts.FortressZone) error {
	piece := pieceAt(board, square)
	if piece == nil {
		return nil
	}
	nextBoard := cloneBoard(board)
	nextBoard[square.Row][square.Col] = nil
	king := findKing(nextBoard, piece.Color)
	if king != nil && isAttacked(nextBoard, *king, opposite(piece.Color), fortressZones) {
		return errors.New("removal would leave king in check")
	}
	return nil
}

func ensureRemovalDoesNotCreateCheck(board [][]*contracts.Piece, target contracts.Square, ownerColor string, fortressZones []contracts.FortressZone) error {
	nextBoard := cloneBoard(board)
	nextBoard[target.Row][target.Col] = nil

	ownerKing := findKing(nextBoard, ownerColor)
	if ownerKing != nil && isAttacked(nextBoard, *ownerKing, opposite(ownerColor), fortressZones) {
		return errors.New("cannot remove that piece because it would leave your king in check")
	}

	enemyColor := opposite(ownerColor)
	enemyKing := findKing(nextBoard, enemyColor)
	if enemyKing != nil && isAttacked(nextBoard, *enemyKing, ownerColor, fortressZones) {
		return errors.New("cannot remove that piece because it would leave enemy king in check")
	}

	return nil
}

func safeTransformOptions(board [][]*contracts.Piece, target contracts.Square, mechanic string, fortressZones []contracts.FortressZone) []string {
	piece := pieceAt(board, target)
	if piece == nil {
		return nil
	}

	options := transformOptions(piece.Type, mechanic)
	if len(options) == 0 {
		return nil
	}

	safe := make([]string, 0, len(options))
	for _, option := range options {
		nextBoard := cloneBoard(board)
		nextPiece := nextBoard[target.Row][target.Col]
		if nextPiece == nil {
			continue
		}
		nextPiece.Type = option

		if !kingsRemainSafe(nextBoard, fortressZones) {
			continue
		}

		safe = append(safe, option)
	}

	return safe
}

func transformOptions(pieceType string, mechanic string) []string {
	switch mechanic {
	case "promote", "promotehim":
		switch pieceType {
		case "pawn":
			return []string{"knight", "bishop", "rook", "queen"}
		case "knight":
			return []string{"bishop", "rook", "queen"}
		case "bishop":
			return []string{"knight", "rook", "queen"}
		case "rook":
			return []string{"queen"}
		}
	case "demote", "demotehim":
		switch pieceType {
		case "queen":
			return []string{"rook", "bishop", "knight", "pawn"}
		case "rook":
			return []string{"bishop", "knight", "pawn"}
		case "bishop":
			return []string{"knight", "pawn"}
		case "knight":
			return []string{"pawn"}
		}
	}

	return nil
}

func validateTransformTarget(piece *contracts.Piece, ownerColor string, mechanic string, target contracts.Square) error {
	if piece == nil || piece.Type == "king" {
		switch mechanic {
		case "promotehim":
			return errors.New("promotehim requires an enemy non-king target")
		case "demotehim":
			return errors.New("demotehim requires a non-king target")
		case "promote":
			return errors.New("promote requires your own non-king target")
		default:
			return errors.New("demote requires your own non-king target")
		}
	}

	switch mechanic {
	case "promote":
		if piece.Color != ownerColor {
			return errors.New("promote requires your own non-king target")
		}
	case "demote":
		if piece.Color != ownerColor {
			return errors.New("demote requires your own non-king target")
		}
	case "promotehim":
		if piece.Color == ownerColor {
			return errors.New("promotehim requires an enemy non-king target")
		}
	case "demotehim":
		return nil
	}

	return nil
}

func isPawnOnPromotionRanks(row int, color string) bool {
	if color == "white" {
		return row >= 6
	}
	return row <= 1
}

func kingsRemainSafe(board [][]*contracts.Piece, fortressZones []contracts.FortressZone) bool {
	whiteKing := findKing(board, "white")
	if whiteKing != nil && isAttacked(board, *whiteKing, "black", fortressZones) {
		return false
	}
	blackKing := findKing(board, "black")
	if blackKing != nil && isAttacked(board, *blackKing, "white", fortressZones) {
		return false
	}
	return true
}

func kingsRemainSafeWithFusion(board [][]*contracts.Piece, fortressZones []contracts.FortressZone) bool {
	whiteKing := findKing(board, "white")
	if whiteKing != nil && isAttackedWithFusion(board, *whiteKing, "black", fortressZones) {
		return false
	}
	blackKing := findKing(board, "black")
	if blackKing != nil && isAttackedWithFusion(board, *blackKing, "white", fortressZones) {
		return false
	}
	return true
}

func isAttackedWithFusion(board [][]*contracts.Piece, target contracts.Square, by string, fortressZones []contracts.FortressZone) bool {
	if isAttacked(board, target, by, fortressZones) {
		return true
	}
	for r := 0; r < len(board); r++ {
		for c := 0; c < len(board[r]); c++ {
			piece := board[r][c]
			if piece == nil || piece.Color != by || piece.FusedWith == "" || piece.Fake {
				continue
			}
			tempBoard := cloneBoard(board)
			tempBoard[r][c] = &contracts.Piece{
				Type:           piece.FusedWith,
				Color:          piece.Color,
				Shielded:       piece.Shielded,
				ShieldTurn:     piece.ShieldTurn,
				Frozen:         piece.Frozen,
				Borrowed:       piece.Borrowed,
				ParasiteTarget: piece.ParasiteTarget,
				Bomb:           piece.Bomb,
				Invisible:      piece.Invisible,
				InvisibleTurn:  piece.InvisibleTurn,
				InvisibleOver:  piece.InvisibleOver,
			}
			if isAttacked(tempBoard, target, by, fortressZones) {
				return true
			}
		}
	}
	return false
}

func fusionRedundancy(typeA string, typeB string, sqA contracts.Square, sqB contracts.Square) string {
	if typeA == typeB {
		return "cannot fuse identical piece types"
	}
	if (typeA == "queen" && typeB == "rook") || (typeA == "rook" && typeB == "queen") {
		return "queen already moves like a rook"
	}
	if (typeA == "queen" && typeB == "bishop") || (typeA == "bishop" && typeB == "queen") {
		return "queen already moves like a bishop"
	}
	if (typeA == "queen" && typeB == "pawn") || (typeA == "pawn" && typeB == "queen") {
		return "queen already outclasses pawn movement"
	}
	if typeA == "bishop" && typeB == "bishop" && ((sqA.Row+sqA.Col)%2 == (sqB.Row+sqB.Col)%2) {
		return "bishops on the same color add no new movement"
	}
	return ""
}

func jumpHasExactlyOnePieceBetween(board [][]*contracts.Piece, from contracts.Square, to contracts.Square) bool {
	dr := to.Row - from.Row
	dc := to.Col - from.Col
	if dr == 0 && dc == 0 {
		return false
	}

	sr := sign(dr)
	sc := sign(dc)
	r := from.Row + sr
	c := from.Col + sc
	count := 0
	for r != to.Row || c != to.Col {
		if !inBounds(r, c) {
			return false
		}
		if board[r][c] != nil {
			count++
		}
		r += sr
		c += sc
	}

	return count == 1
}

func jumpDirectionValid(from contracts.Square, to contracts.Square, pieceType string, pieceColor string) bool {
	dr := to.Row - from.Row
	dc := to.Col - from.Col
	diag := abs(dr) == abs(dc)
	straight := dr == 0 || dc == 0

	switch pieceType {
	case "bishop":
		return diag
	case "rook":
		return straight
	case "queen":
		return diag || straight
	case "pawn":
		fwd := 1
		if pieceColor == "black" {
			fwd = -1
		}
		return (dc == 0 && (dr == fwd || dr == fwd*2)) || (abs(dc) == 2 && dr == fwd*2)
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
