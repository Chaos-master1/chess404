package engine

import (
	"math/rand"

	"github.com/chess404/realtime/internal/contracts"
)

type CardEvaluator struct {
	rng      *rand.Rand
	engine   *NNUE
}

func NewCardEvaluator(rng *rand.Rand) *CardEvaluator {
	return &CardEvaluator{rng: rng, engine: defaultNNUE}
}

type CardPlay struct {
	Card     contracts.GameCard
	Score    int
	Target   *contracts.Square
	Decision string
}

func (ce *CardEvaluator) EvaluateHand(hand []contracts.GameCard, state *contracts.MatchState, isWhite bool) []CardPlay {
	plays := make([]CardPlay, 0, len(hand))
	for _, card := range hand {
		play := ce.evaluateCard(card, state, isWhite)
		plays = append(plays, play)
	}
	return plays
}

// evalDiff computes the score change from playing a card by simulating the effect on a clone.
func (ce *CardEvaluator) evalDiff(state *contracts.MatchState, color string, apply func(*contracts.MatchState)) int {
	cloned := cloneMatchState(state)
	apply(cloned)

	base := Evaluate(state.Board, state.Turn, state.WhiteHand, state.BlackHand)
	after := Evaluate(cloned.Board, cloned.Turn, cloned.WhiteHand, cloned.BlackHand)
	mult := 1
	if color == "black" {
		mult = -1
	}
	return (after - base) * mult
}

func (ce *CardEvaluator) evaluateCard(card contracts.GameCard, state *contracts.MatchState, isWhite bool) CardPlay {
	score := 0
	color := "black"
	if isWhite {
		color = "white"
	}

	switch card.Mechanic {
	case "freeze":
		score = ce.freezeValue(state, color)
	case "shield":
		score = ce.shieldValue(state, color)
	case "sniper":
		score = ce.sniperValue(state, color)
	case "badsniper":
		score = -ce.sniperValue(state, color) / 2
	case "teleport":
		score = 50 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			teleportEffect(s, color)
		})
	case "jump":
		score = 40 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			jumpEffect(s, color)
		})
	case "swapme":
		score = ce.swapMeValue(state, color)
	case "swapus":
		score = ce.swapUsValue(state, color)
	case "swaphim":
		score = ce.swapHimValue(state, color)
	case "clone":
		score = 50 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			cloneEffect(s, color)
		})
	case "halffuse":
		score = 60 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			fuseEffect(s, color, true)
		})
	case "fullfusion":
		score = 80 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			fuseEffect(s, color, false)
		})
	case "doublemove_same", "doublemove_diff":
		score = 70 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			doubleMoveEffect(s, color)
		})
	case "promote":
		score = ce.promoteValue(state, color)
	case "demote":
		score = ce.demoteValue(state, color)
	case "demotehim":
		score = ce.demoteValue(state, oppositeColor(color))
	case "promotehim":
		score = -30
	case "mindcontrol":
		score = 100 + ce.mindControlValue(state, color)
	case "borrow":
		score = 70 + ce.borrowValue(state, color)
	case "reverse":
		score = ce.reverseValue(state, color)
	case "undo":
		score = 50
	case "mirror":
		score = 40
	case "invisible":
		score = 45
	case "lavaground":
		score = 35
	case "fog_village":
		score = 30
	case "fortress":
		score = 55
	case "radar":
		score = 25
	case "unabomber":
		score = 60 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			unabomberEffect(s, color)
		})
	case "blackhole":
		score = 65 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			blackholeEffect(s, color)
		})
	case "parasite":
		score = 55 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			parasiteEffect(s, color)
		})
	case "fakepiece":
		score = 20 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			fakePieceEffect(s, color)
		})
	case "cheater":
		score = 30 + ce.evalDiff(state, color, func(s *contracts.MatchState) {
			cheaterEffect(s, color)
		})
	case "gambler":
		score = 10
	case "smallsacrifice":
		score = ce.sacrificeValue(state, color, 6)
	case "bigsacrifice":
		score = ce.sacrificeValue(state, color, 14)
	case "joker":
		score = 75
	}

	return CardPlay{
		Card:  card,
		Score: score,
	}
}

func (ce *CardEvaluator) ShouldPlayCard(state *contracts.MatchState, isWhite bool) bool {
	if state.Status != "active" {
		return false
	}

	hand := state.BlackHand
	if isWhite {
		hand = state.WhiteHand
	}
	if len(hand) == 0 {
		return false
	}

	plays := ce.EvaluateHand(hand, state, isWhite)
	if len(plays) == 0 {
		return false
	}

	bestScore := plays[0].Score
	for _, p := range plays[1:] {
		if p.Score > bestScore {
			bestScore = p.Score
		}
	}

	if bestScore >= 50 {
		return true
	}

	if len(hand) >= 5 && bestScore >= 30 {
		return true
	}

	return false
}

func (ce *CardEvaluator) BestCardToPlay(state *contracts.MatchState, isWhite bool) *CardPlay {
	hand := state.BlackHand
	if isWhite {
		hand = state.WhiteHand
	}
	if len(hand) == 0 {
		return nil
	}

	plays := ce.EvaluateHand(hand, state, isWhite)
	if len(plays) == 0 {
		return nil
	}

	best := &plays[0]
	for i := range plays[1:] {
		if plays[i+1].Score > best.Score {
			best = &plays[i+1]
		}
	}

	if best.Score < 20 {
		return nil
	}

	return best
}

func (ce *CardEvaluator) freezeValue(state *contracts.MatchState, color string) int {
	opponent := oppositeColor(color)
	bestValue := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece != nil && piece.Color == opponent && piece.Type != "king" && !piece.Frozen {
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
				}
			}
		}
	}
	return bestValue / 5
}

func (ce *CardEvaluator) shieldValue(state *contracts.MatchState, color string) int {
	bestValue := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece != nil && piece.Color == color && piece.Type != "king" && !piece.Shielded {
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
				}
			}
		}
	}
	return bestValue / 4
}

func (ce *CardEvaluator) sniperValue(state *contracts.MatchState, color string) int {
	opponent := oppositeColor(color)
	bestValue := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece != nil && piece.Color == opponent && piece.Type != "king" {
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
				}
			}
		}
	}
	return bestValue / 3
}

func (ce *CardEvaluator) swapMeValue(state *contracts.MatchState, color string) int {
	bestDiff := 0
	for r1 := 0; r1 < 8; r1++ {
		for c1 := 0; c1 < 8; c1++ {
			p1 := state.Board[r1][c1]
			if p1 == nil || p1.Color != color || p1.Type == "king" {
				continue
			}
			for r2 := 0; r2 < 8; r2++ {
				for c2 := 0; c2 < 8; c2++ {
					p2 := state.Board[r2][c2]
					if p2 == nil || p2.Color != color || p2.Type == "king" {
						continue
					}
					if r1 == r2 && c1 == c2 {
						continue
					}
					diff := positionalBonus(p2, r1, c1, false) - positionalBonus(p2, r2, c2, false)
					if diff > bestDiff {
						bestDiff = diff
					}
				}
			}
		}
	}
	return bestDiff / 2
}

func (ce *CardEvaluator) swapUsValue(state *contracts.MatchState, color string) int {
	opponent := oppositeColor(color)
	bestDiff := 0
	for r1 := 0; r1 < 8; r1++ {
		for c1 := 0; c1 < 8; c1++ {
			p1 := state.Board[r1][c1]
			if p1 == nil || p1.Color != color || p1.Type == "king" {
				continue
			}
			for r2 := 0; r2 < 8; r2++ {
				for c2 := 0; c2 < 8; c2++ {
					p2 := state.Board[r2][c2]
					if p2 == nil || p2.Color != opponent || p2.Type == "king" {
						continue
					}
					diff := pieceValue(p2.Type) - pieceValue(p1.Type)
					if diff > bestDiff {
						bestDiff = diff
					}
				}
			}
		}
	}
	return bestDiff / 2
}

func (ce *CardEvaluator) swapHimValue(state *contracts.MatchState, color string) int {
	opponent := oppositeColor(color)
	bestDiff := 0
	for r1 := 0; r1 < 8; r1++ {
		for c1 := 0; c1 < 8; c1++ {
			p1 := state.Board[r1][c1]
			if p1 == nil || p1.Color != opponent || p1.Type == "king" {
				continue
			}
			for r2 := 0; r2 < 8; r2++ {
				for c2 := 0; c2 < 8; c2++ {
					p2 := state.Board[r2][c2]
					if p2 == nil || p2.Color != opponent || p2.Type == "king" {
						continue
					}
					if r1 == r2 && c1 == c2 {
						continue
					}
					diff := positionalBonus(p2, r1, c1, false) - positionalBonus(p2, r2, c2, false)
					if diff > bestDiff {
						bestDiff = diff
					}
				}
			}
		}
	}
	return bestDiff / 3
}

func (ce *CardEvaluator) promoteValue(state *contracts.MatchState, color string) int {
	bestValue := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece != nil && piece.Color == color && piece.Type != "king" && piece.Type != "queen" {
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
				}
			}
		}
	}
	return bestValue / 3
}

func (ce *CardEvaluator) demoteValue(state *contracts.MatchState, color string) int {
	bestValue := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece != nil && piece.Color == color && piece.Type != "king" && piece.Type != "pawn" {
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
				}
			}
		}
	}
	return bestValue / 3
}

func (ce *CardEvaluator) reverseValue(state *contracts.MatchState, color string) int {
	if state.LastMove == nil {
		return 0
	}
	piece := state.Board[state.LastMove.To.Row][state.LastMove.To.Col]
	if piece == nil || piece.Color == color {
		return 0
	}
	return pieceValue(piece.Type) / 2
}

func (ce *CardEvaluator) sacrificeValue(state *contracts.MatchState, color string, threshold int) int {
	totalValue := 0
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece != nil && piece.Color == color && piece.Type != "king" {
				totalValue += pieceValue(piece.Type)
			}
		}
	}
	if totalValue >= threshold*100 {
		return 50
	}
	return 0
}

func (ce *CardEvaluator) mindControlValue(state *contracts.MatchState, color string) int { return 0 }
func (ce *CardEvaluator) borrowValue(state *contracts.MatchState, color string) int      { return 0 }

// ---- Card effect simulators (for evalDiff) ----

func teleportEffect(s *contracts.MatchState, color string) {
	// Move the most advanced piece to a more active square.
	if s.Turn != color {
		s.Turn = color
	}
}

func jumpEffect(s *contracts.MatchState, color string) {
	if s.Turn != color {
		s.Turn = color
	}
}

func cloneEffect(s *contracts.MatchState, color string) {
	// Simulate: find strongest piece and clone it to an empty square.
	empty := findEmptySquares(s.Board)
	if len(empty) == 0 {
		return
	}
	bestVal := 0
	var bestPiece *contracts.Piece
	var bestR, bestC int
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := s.Board[r][c]
			if p != nil && p.Color == color && p.Type != "king" {
				v := pieceValue(p.Type)
				if v > bestVal {
					bestVal = v
					bestPiece = p
					bestR, bestC = r, c
				}
			}
		}
	}
	if bestPiece != nil {
		clone := &contracts.Piece{Type: bestPiece.Type, Color: color}
		s.Board[empty[0].Row][empty[0].Col] = clone
		_ = bestR
		_ = bestC
	}
}

func fuseEffect(s *contracts.MatchState, color string, half bool) {
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := s.Board[r][c]
			if p != nil && p.Color == color {
				if half && p.FusedWith == "" {
					p.FusedWith = p.Type
				} else if !half {
					p.FusedWith = "queen"
				}
			}
		}
	}
}

func doubleMoveEffect(s *contracts.MatchState, color string) {
	// Already accounted for by giving the player an extra turn.
	if s.Turn != color {
		s.Turn = color
	}
}

func unabomberEffect(s *contracts.MatchState, color string) {
	opp := oppositeColor(color)
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := s.Board[r][c]
			if p != nil && p.Color == opp {
				s.Board[r][c] = nil
			}
		}
	}
}

func blackholeEffect(s *contracts.MatchState, color string) {
	// Remove pieces near center as a rough approximation.
	for r := 2; r <= 5; r++ {
		for c := 2; c <= 5; c++ {
			if s.Board[r][c] != nil {
				s.Board[r][c] = nil
			}
		}
	}
}

func parasiteEffect(s *contracts.MatchState, color string) {
	opp := oppositeColor(color)
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := s.Board[r][c]
			if p != nil && p.Color == opp && p.Type != "king" {
				s.Board[r][c] = nil
			}
		}
	}
}

func fakePieceEffect(s *contracts.MatchState, color string) {
	empty := findEmptySquares(s.Board)
	if len(empty) > 0 {
		s.Board[empty[0].Row][empty[0].Col] = &contracts.Piece{Type: "pawn", Color: color}
	}
}

func cheaterEffect(s *contracts.MatchState, color string) {
	opp := oppositeColor(color)
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := s.Board[r][c]
			if p != nil && p.Color == opp {
				p.FusedWith = ""
				p.Shielded = false
			}
		}
	}
}

func findEmptySquares(board [][]*contracts.Piece) []contracts.Square {
	var sq []contracts.Square
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			if board[r][c] == nil {
				sq = append(sq, contracts.Square{Row: r, Col: c})
			}
		}
	}
	return sq
}
