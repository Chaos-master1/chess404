package engine

import (
	"github.com/chess404/realtime/internal/contracts"
)

var pieceValues = map[string]int{
	"pawn":   100,
	"knight": 320,
	"bishop": 330,
	"rook":   500,
	"queen":  900,
	"king":   20000,
}

var pawnTable = [8][8]int{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{50, 50, 50, 50, 50, 50, 50, 50},
	{10, 10, 20, 30, 30, 20, 10, 10},
	{5, 5, 10, 25, 25, 10, 5, 5},
	{0, 0, 0, 20, 20, 0, 0, 0},
	{5, -5, -10, 0, 0, -10, -5, 5},
	{5, 10, 10, -20, -20, 10, 10, 5},
	{0, 0, 0, 0, 0, 0, 0, 0},
}

var knightTable = [8][8]int{
	{-50, -40, -30, -30, -30, -30, -40, -50},
	{-40, -20, 0, 0, 0, 0, -20, -40},
	{-30, 0, 10, 15, 15, 10, 0, -30},
	{-30, 5, 15, 20, 20, 15, 5, -30},
	{-30, 0, 15, 20, 20, 15, 0, -30},
	{-30, 5, 10, 15, 15, 10, 5, -30},
	{-40, -20, 0, 5, 5, 0, -20, -40},
	{-50, -40, -30, -30, -30, -30, -40, -50},
}

var bishopTable = [8][8]int{
	{-20, -10, -10, -10, -10, -10, -10, -20},
	{-10, 0, 0, 0, 0, 0, 0, -10},
	{-10, 0, 10, 10, 10, 10, 0, -10},
	{-10, 5, 5, 10, 10, 5, 5, -10},
	{-10, 0, 10, 10, 10, 10, 0, -10},
	{-10, 10, 10, 10, 10, 10, 10, -10},
	{-10, 5, 0, 0, 0, 0, 5, -10},
	{-20, -10, -10, -10, -10, -10, -10, -20},
}

var rookTable = [8][8]int{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{5, 10, 10, 10, 10, 10, 10, 5},
	{-5, 0, 0, 0, 0, 0, 0, -5},
	{-5, 0, 0, 0, 0, 0, 0, -5},
	{-5, 0, 0, 0, 0, 0, 0, -5},
	{-5, 0, 0, 0, 0, 0, 0, -5},
	{-5, 0, 0, 0, 0, 0, 0, -5},
	{0, 0, 0, 5, 5, 0, 0, 0},
}

var queenTable = [8][8]int{
	{-20, -10, -10, -5, -5, -10, -10, -20},
	{-10, 0, 0, 0, 0, 0, 0, -10},
	{-10, 0, 5, 5, 5, 5, 0, -10},
	{-5, 0, 5, 5, 5, 5, 0, -5},
	{0, 0, 5, 5, 5, 5, 0, -5},
	{-10, 5, 5, 5, 5, 5, 0, -10},
	{-10, 0, 5, 0, 0, 0, 0, -10},
	{-20, -10, -10, -5, -5, -10, -10, -20},
}

var kingMiddleTable = [8][8]int{
	{-30, -40, -40, -50, -50, -40, -40, -30},
	{-30, -40, -40, -50, -50, -40, -40, -30},
	{-30, -40, -40, -50, -50, -40, -40, -30},
	{-30, -40, -40, -50, -50, -40, -40, -30},
	{-20, -30, -30, -40, -40, -30, -30, -20},
	{-10, -20, -20, -20, -20, -20, -20, -10},
	{20, 20, 0, 0, 0, 0, 20, 20},
	{20, 30, 10, 0, 0, 10, 30, 20},
}

var kingEndTable = [8][8]int{
	{-50, -40, -30, -20, -20, -30, -40, -50},
	{-30, -20, -10, 0, 0, -10, -20, -30},
	{-30, -10, 20, 30, 30, 20, -10, -30},
	{-30, -10, 30, 40, 40, 30, -10, -30},
	{-30, -10, 30, 40, 40, 30, -10, -30},
	{-30, -10, 20, 30, 30, 20, -10, -30},
	{-30, -30, 0, 0, 0, 0, -30, -30},
	{-50, -30, -30, -30, -30, -30, -30, -50},
}

// isLavaSquare checks if a given square has an active lava trap.
func isLavaSquare(lavas []contracts.LavaSquare, row, col int) bool {
	for _, lava := range lavas {
		if lava.Row == row && lava.Col == col {
			return true
		}
	}
	return false
}

// inFriendlyFortress checks if a square is inside a fortress zone owned by color.
func inFriendlyFortress(zones []contracts.FortressZone, color string, row, col int) bool {
	for _, z := range zones {
		if z.OwnerColor != color {
			continue
		}
		if row >= z.TopRow && row <= z.TopRow+1 && col >= z.LeftCol && col <= z.LeftCol+1 {
			return true
		}
	}
	return false
}

func Evaluate(board [][]*contracts.Piece, turn string) int {
	return EvaluateWithModifiers(board, turn, nil, nil, nil)
}

// EvaluateWithModifiers extends Evaluate with board-modifier scoring (lava, fortress, bombs).
// Uses NNUE if weights are loaded, otherwise falls back to hand-crafted evaluation.
func EvaluateWithModifiers(board [][]*contracts.Piece, turn string, lavas []contracts.LavaSquare, fortresses []contracts.FortressZone, bombs []contracts.BombPiece) int {
	if defaultNNUE != nil && defaultNNUE.Loaded() {
		nnue := defaultNNUE.Evaluate(board, lavas, fortresses, bombs, nil, nil)
		if turn == "black" {
			nnue = -nnue
		}
		return nnue
	}
	score := 0
	whiteMaterial := 0
	blackMaterial := 0

	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := board[r][c]
			if piece == nil {
				continue
			}

			value := pieceValue(piece.Type)
			if piece.FusedWith != "" {
				value = (value + pieceValue(piece.FusedWith)) / 2
			}

			if piece.Color == "white" {
				whiteMaterial += value
			} else {
				blackMaterial += value
			}
		}
	}

	isEndgame := whiteMaterial+blackMaterial < 2600

	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := board[r][c]
			if piece == nil {
				continue
			}

			value := pieceValue(piece.Type)
			if piece.FusedWith != "" {
				value = (value + pieceValue(piece.FusedWith)) / 2
			}

			posBonus := positionalBonus(piece, r, c, isEndgame)

			total := value + posBonus

			if piece.Color == turn {
				score += total
			} else {
				score -= total
			}
		}
	}

	// --- Pawn structure evaluation ---
	whitePawnSquares := pawnSquares(board, "white")
	blackPawnSquares := pawnSquares(board, "black")
	score += pawnStructureScore(board, whitePawnSquares)
	score -= pawnStructureScore(board, blackPawnSquares)

	// Passed pawn bonuses
	score += passedPawnBonus(board, whitePawnSquares, blackPawnSquares, true)
	score -= passedPawnBonus(board, blackPawnSquares, whitePawnSquares, false)

	// Knight outpost bonus
	score += outpostBonus(board, "white")
	score -= outpostBonus(board, "black")

	whiteKing := findKingPos(board, "white")
	blackKing := findKingPos(board, "black")

	if whiteKing != nil {
		whiteKingShield := kingShieldScore(board, whiteKing.Row, whiteKing.Col, "white")
		if isEndgame {
			whiteKingShield = 0
		}
		if turn == "white" {
			score += whiteKingShield
		} else {
			score -= whiteKingShield
		}
	}

	if blackKing != nil {
		blackKingShield := kingShieldScore(board, blackKing.Row, blackKing.Col, "black")
		if isEndgame {
			blackKingShield = 0
		}
		if turn == "white" {
			score -= blackKingShield
		} else {
			score += blackKingShield
		}
	}

	// --- Board-modifier scoring ---

	for _, lava := range lavas {
		if lava.Row < 0 || lava.Row > 7 || lava.Col < 0 || lava.Col > 7 {
			continue
		}
		piece := board[lava.Row][lava.Col]
		if piece == nil {
			continue
		}
		penalty := pieceValue(piece.Type) / 3
		if piece.Color == turn {
			score -= penalty
		} else {
			score += penalty
		}
	}

	for _, z := range fortresses {
		if z.OwnerColor == turn {
			score += 30
		} else {
			score -= 30
		}
	}

	for _, bomb := range bombs {
		ownBomb := bomb.OwnerColor == turn
		for dr := -1; dr <= 1; dr++ {
			for dc := -1; dc <= 1; dc++ {
				r := bomb.Row + dr
				c := bomb.Col + dc
				if !inBounds(r, c) || (dr == 0 && dc == 0) {
					continue
				}
				p := board[r][c]
				if p == nil || p.Type == "king" {
					continue
				}
				if ownBomb && p.Color == turn {
					score -= pieceValue(p.Type) / 4
				} else if ownBomb && p.Color != turn {
					score += pieceValue(p.Type) / 4
				}
			}
		}
	}

	return score
}

// pawnSquares returns a set of column indices per row for pawns of the given color.
func pawnSquares(board [][]*contracts.Piece, color string) []uint8 {
	cols := make([]uint8, 8)
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := board[r][c]
			if piece != nil && piece.Type == "pawn" && piece.Color == color {
				cols[r] |= 1 << uint(c)
			}
		}
	}
	return cols
}

// pawnStructureScore penalizes doubled and isolated pawns, rewards connected pawns.
func pawnStructureScore(board [][]*contracts.Piece, pawnCols []uint8) int {
	score := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			if pawnCols[r]&(1<<uint(c)) == 0 {
				continue
			}
			// Isolated pawn: no friendly pawns on adjacent files
			isolated := true
			for adj := max(0, c-1); adj <= min(7, c+1); adj++ {
				if adj == c {
					continue
				}
				for rr := 0; rr < 8; rr++ {
					if pawnCols[rr]&(1<<uint(adj)) != 0 {
						isolated = false
						break
					}
				}
				if !isolated {
					break
				}
			}
			if isolated {
				score -= 15
			}

			// Doubled pawn: another pawn in same file
			doubled := false
			for rr := 0; rr < 8; rr++ {
				if rr == r {
					continue
				}
				if pawnCols[rr]&(1<<uint(c)) != 0 {
					doubled = true
					break
				}
			}
			if doubled {
				score -= 10
			}

			// Connected pawn: friendly pawn on adjacent file in neighboring rows
			connected := false
			for adj := max(0, c-1); adj <= min(7, c+1); adj++ {
				if adj == c {
					continue
				}
				for dr := -1; dr <= 1; dr++ {
					rr := r + dr
					if rr < 0 || rr > 7 {
						continue
					}
					if pawnCols[rr]&(1<<uint(adj)) != 0 {
						connected = true
						break
					}
				}
				if connected {
					break
				}
			}
			if connected {
				score += 8
			}
		}
	}
	return score
}

// passedPawnBonus rewards pawns with no enemy pawns blocking or opposing on the same or adjacent files.
func passedPawnBonus(board [][]*contracts.Piece, myPawns, enemyPawns []uint8, isWhite bool) int {
	score := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			if myPawns[r]&(1<<uint(c)) == 0 {
				continue
			}
			passed := true
			start := r + 1
			end := 8
			if !isWhite {
				start = r - 1
				end = -1
			}
			step := 1
			if !isWhite {
				step = -1
			}
			for rr := start; rr != end; rr += step {
				if enemyPawns[rr]&(1<<uint(c)) != 0 {
					passed = false
					break
				}
				if c > 0 && enemyPawns[rr]&(1<<uint(c-1)) != 0 {
					passed = false
					break
				}
				if c < 7 && enemyPawns[rr]&(1<<uint(c+1)) != 0 {
					passed = false
					break
				}
			}
			if passed {
				dist := r
				if !isWhite {
					dist = 7 - r
				}
				bonus := 10 + dist*5
				score += bonus
			}
		}
	}
	return score
}

// outpostBonus rewards knights on squares protected by a friendly pawn with no enemy pawns that can attack it.
func outpostBonus(board [][]*contracts.Piece, color string) int {
	score := 0
	enemy := oppositeColor(color)
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := board[r][c]
			if piece == nil || piece.Type != "knight" || piece.Color != color {
				continue
			}
			hasPawnCover := false
			pawnRow := r + 1
			if color == "black" {
				pawnRow = r - 1
			}
			if pawnRow >= 0 && pawnRow < 8 {
				for dc := -1; dc <= 1; dc++ {
					pc := c + dc
					if pc < 0 || pc > 7 {
						continue
					}
					p := board[pawnRow][pc]
					if p != nil && p.Type == "pawn" && p.Color == color {
						hasPawnCover = true
						break
					}
				}
			}
			if !hasPawnCover {
				continue
			}
			canBeAttacked := false
			for dr := -1; dr <= 1; dr++ {
				for dc := -1; dc <= 1; dc++ {
					if dr == 0 && dc == 0 {
						continue
					}
					pr := r + dr
					pc := c + dc
					if pr < 0 || pr > 7 || pc < 0 || pc > 7 {
						continue
					}
					p := board[pr][pc]
					if p != nil && p.Type == "pawn" && p.Color == enemy {
						canBeAttacked = true
						break
					}
				}
				if canBeAttacked {
					break
				}
			}
			if !canBeAttacked {
				score += 20
			}
		}
	}
	return score
}



func positionalBonus(piece *contracts.Piece, r, c int, isEndgame bool) int {
	blackRow := 7 - r
	switch piece.Type {
	case "pawn":
		if piece.Color == "white" {
			return pawnTable[r][c]
		}
		return pawnTable[blackRow][c]
	case "knight":
		if piece.Color == "white" {
			return knightTable[r][c]
		}
		return knightTable[blackRow][c]
	case "bishop":
		if piece.Color == "white" {
			return bishopTable[r][c]
		}
		return bishopTable[blackRow][c]
	case "rook":
		if piece.Color == "white" {
			return rookTable[r][c]
		}
		return rookTable[blackRow][c]
	case "queen":
		if piece.Color == "white" {
			return queenTable[r][c]
		}
		return queenTable[blackRow][c]
	case "king":
		if isEndgame {
			if piece.Color == "white" {
				return kingEndTable[r][c]
			}
			return kingEndTable[blackRow][c]
		}
		if piece.Color == "white" {
			return kingMiddleTable[r][c]
		}
		return kingMiddleTable[blackRow][c]
	}
	return 0
}

func kingShieldScore(board [][]*contracts.Piece, kingRow, kingCol int, color string) int {
	score := 0
	dir := 1
	if color == "black" {
		dir = -1
	}
	for dr := 0; dr <= 2; dr++ {
		for dc := -1; dc <= 1; dc++ {
			r := kingRow + dr*dir
			c := kingCol + dc
			if r < 0 || r > 7 || c < 0 || c > 7 {
				continue
			}
			piece := board[r][c]
			if piece != nil && piece.Color == color && piece.Type == "pawn" {
				score += 10
			}
			if piece != nil && piece.Color == color && (piece.Type == "knight" || piece.Type == "bishop") {
				score += 5
			}
		}
	}
	return score
}

func findKingPos(board [][]*contracts.Piece, color string) *contracts.Square {
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := board[r][c]
			if piece != nil && piece.Type == "king" && piece.Color == color {
				return &contracts.Square{Row: r, Col: c}
			}
		}
	}
	return nil
}

func pieceValue(pieceType string) int {
	if v, ok := pieceValues[pieceType]; ok {
		return v
	}
	return 0
}
