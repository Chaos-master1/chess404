package engine

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

const (
	timeBeginner = 100 * time.Millisecond
	timeEasy     = 250 * time.Millisecond
	timeMedium   = 500 * time.Millisecond
	timeHard     = 1000 * time.Millisecond
	timeExpert   = 2000 * time.Millisecond
)

type Difficulty int

const (
	DifficultyBeginner Difficulty = iota
	DifficultyEasy
	DifficultyMedium
	DifficultyHard
	DifficultyExpert
)

func ParseDifficulty(s string) Difficulty {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "beginner":
		return DifficultyBeginner
	case "easy":
		return DifficultyEasy
	case "hard":
		return DifficultyHard
	case "expert":
		return DifficultyExpert
	default:
		return DifficultyMedium
	}
}

func (d Difficulty) SearchDepth() int {
	return 32
}

func (d Difficulty) TimeLimit() time.Duration {
	switch d {
	case DifficultyBeginner:
		return timeBeginner
	case DifficultyEasy:
		return timeEasy
	case DifficultyMedium:
		return timeMedium
	case DifficultyHard:
		return timeHard
	case DifficultyExpert:
		return timeExpert
	default:
		return timeMedium
	}
}

func (d Difficulty) ShouldPlayCard(card contracts.GameCard, score int) bool {
	switch d {
	case DifficultyBeginner:
		return score >= 60 && rand.Float64() < 0.3
	case DifficultyEasy:
		return score >= 50 && rand.Float64() < 0.5
	case DifficultyMedium:
		return score >= 40
	case DifficultyHard:
		return score >= 30
	case DifficultyExpert:
		return score >= 20
	default:
		return score >= 40
	}
}

type ComputerOpponent struct {
	Difficulty Difficulty
	Color      string
	rng        *rand.Rand
	tt         *TranspositionTable
	cardEval   *CardEvaluator
	mu         sync.Mutex
}

func NewComputerOpponent(difficulty Difficulty, color string) *ComputerOpponent {
	seed := time.Now().UnixNano()
	return &ComputerOpponent{
		Difficulty: difficulty,
		Color:      color,
		rng:        rand.New(rand.NewSource(seed)),
		tt:         NewTranspositionTable(1 << 16),
		cardEval:   NewCardEvaluator(rand.New(rand.NewSource(seed + 1))),
	}
}

// computerPlayableMechanics is every mechanic HandleSelectTarget/findBestTarget
// can actually carry through to a successful select_target (or that needs no
// target at all). It is deliberately a strict subset of the 37 mechanics --
// see the comment on filterHandForComputer for why this exists.
var computerPlayableMechanics = map[string]bool{
	// Zero-target: applyPlayCard resolves these immediately, no PendingCard,
	// so there is nothing to fail.
	"doublemove_same": true, "doublemove_diff": true, "undo": true,
	"reverse": true, "mirror": true, "gambler": true, "radar": true, "cheater": true,
	// One-target, and findBestTarget has a real case for it below.
	"freeze": true, "sniper": true, "badsniper": true, "demotehim": true,
	"mindcontrol": true, "borrow": true, "parasite": true, "demote": true,
	"promotehim": true, "promote": true, "clone": true, "invisible": true,
}

// filterHandForComputer returns a copy of hand containing only mechanics the
// computer can actually complete.
//
// This is the fix for a live bug where the computer, once it happened to draw
// and select ANY of the other 17 mechanics, got permanently stuck: a card
// only leaves the hand when select_target successfully resolves it, but for
// these 17, HandleSelectTarget/findBestTarget either has no matching case at
// all (lavaground, fakepiece -- dispatched but unimplemented), never dispatches
// in the first place (shield, unabomber, fortress, fog_village, teleport, jump,
// blackhole, joker, smallsacrifice, bigsacrifice), or submits a SelectionID
// (swapme/swapus/swaphim/halffuse/fullfusion's PendingCard.Options[0]) that
// applySelectTarget never accepts. match_lifecycle.go's
// autoPlayComputerDepthLimited abandons the pending card on failure but never
// removes it from hand, so on the VERY NEXT turn the same card scores highest
// again, gets picked again, fails again -- forever. Because
// Difficulty.ShouldPlayCard is a deterministic score threshold with no
// randomness for Medium/Hard/Expert (Beginner/Easy additionally roll dice
// against it, which is why they mostly didn't show this), once one of these
// difficulties drew one of the 17, it got stuck making zero real chess moves
// for the rest of the game -- burning all 5 retry levels on the same dead
// card every turn and falling through every time to
// ensureComputerMadeProgressLocked's firstLegalMoveForColor fallback, a raw
// board scan starting from the a-file. That fallback IS the reported "pushes
// the a-file pawn, then b-file, then c-file..." behavior; it was never a
// pawn-specific bug, it was the total absence of any other move being tried.
func filterHandForComputer(hand []contracts.GameCard) []contracts.GameCard {
	filtered := make([]contracts.GameCard, 0, len(hand))
	for _, card := range hand {
		if computerPlayableMechanics[card.Mechanic] {
			filtered = append(filtered, card)
		}
	}
	return filtered
}

func (co *ComputerOpponent) MakeMove(state *contracts.MatchState) *contracts.PlayerIntent {
	co.mu.Lock()
	defer co.mu.Unlock()

	if state.Status != "active" {
		return nil
	}

	// Card scoring reads WhiteHand/BlackHand straight off state; evaluate
	// against a filtered copy so a card the computer cannot complete is never
	// even a candidate, instead of catching the failure after the fact.
	cardState := *state
	cardState.WhiteHand = filterHandForComputer(state.WhiteHand)
	cardState.BlackHand = filterHandForComputer(state.BlackHand)

	if co.cardEval.ShouldPlayCard(&cardState, co.Color == "white") {
		play := co.cardEval.BestCardToPlay(&cardState, co.Color == "white")
		if play != nil && co.Difficulty.ShouldPlayCard(play.Card, play.Score) {
			return &contracts.PlayerIntent{
				Type:     "play_card",
				MatchID:  state.MatchID,
				CardID:   play.Card.ID,
			}
		}
	}

	// Probe opening book first (only in opening phase).
	if state.FullMoveNum <= 10 {
		bookMove := defaultBook.Probe(state, co.Color == "white")
		if bookMove != nil && bookMove.From != bookMove.To {
			return &contracts.PlayerIntent{
				Type:    "make_move",
				MatchID: state.MatchID,
				From:    &bookMove.From,
				To:      &bookMove.To,
			}
		}
	}

	searchDepth := co.Difficulty.SearchDepth()
	timeLimit := co.Difficulty.TimeLimit()

	// Dynamic time adjustment based on position complexity.
	stateTurn := state.Turn == "white"
	moves := generateAllMoves(state, stateTurn)
	numMoves := len(moves)
	if numMoves == 0 {
		return nil
	}
	if numMoves == 1 {
		// Only one legal move: play instantly (but still search a little).
		timeLimit = timeLimit / 5
		if timeLimit < 10*time.Millisecond {
			timeLimit = 10 * time.Millisecond
		}
	} else {
		// Count pieces on the board: more pieces = more complex.
		pieceCount := 0
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				if state.Board[r][c] != nil {
					pieceCount++
				}
			}
		}
		// Scale time: more pieces = more time (max 2x), fewer = less (min 0.5x).
		factor := 0.5 + float64(pieceCount)/64.0
		timeLimit = time.Duration(float64(timeLimit) * factor)
		// In complex positions (many legal moves), spend more time.
		if numMoves > 30 {
			timeLimit += timeLimit / 2
		}
	}

	result := SearchWithTime(state, searchDepth, co.tt, timeLimit)

	if result.BestMove.From == (contracts.Square{}) && result.BestMove.To == (contracts.Square{}) {
		return nil
	}

	intent := &contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  state.MatchID,
		From:     &result.BestMove.From,
		To:       &result.BestMove.To,
	}

	return intent
}

func (co *ComputerOpponent) HandleSelectTarget(state *contracts.MatchState) *contracts.PlayerIntent {
	if state.PendingCard == nil {
		return nil
	}

	mechanic := state.PendingCard.Mechanic
	ownerColor := state.PendingCard.OwnerColor

	if mechanic == "freeze" || mechanic == "sniper" || mechanic == "badsniper" ||
		mechanic == "demote" || mechanic == "demotehim" || mechanic == "promote" ||
		mechanic == "promotehim" || mechanic == "mindcontrol" || mechanic == "borrow" ||
		mechanic == "parasite" || mechanic == "lavaground" || mechanic == "clone" ||
		mechanic == "invisible" || mechanic == "fakepiece" {

		target := co.findBestTarget(state, mechanic, ownerColor)
		if target != nil {
			return &contracts.PlayerIntent{
				Type:        "select_target",
				MatchID:     state.MatchID,
				SelectionID: targetSelectionID(mechanic),
				Target:      target,
			}
		}
	}

	if mechanic == "swapme" || mechanic == "swapus" || mechanic == "swaphim" || mechanic == "halffuse" || mechanic == "fullfusion" {
		if state.PendingCard.Options != nil && len(state.PendingCard.Options) > 0 {
			return &contracts.PlayerIntent{
				Type:        "select_target",
				MatchID:     state.MatchID,
				SelectionID: state.PendingCard.Options[0],
			}
		}
	}

	return nil
}

func (co *ComputerOpponent) findBestTarget(state *contracts.MatchState, mechanic, ownerColor string) *contracts.Square {
	opponent := oppositeColor(ownerColor)

	switch mechanic {
	case "freeze", "sniper", "badsniper", "demotehim", "mindcontrol", "borrow", "parasite":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != opponent || piece.Type == "king" {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "demote", "promotehim":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Type == "king" || piece.Type == "pawn" {
					continue
				}
				value := pieceValue(piece.Type)
				if piece.Color == opponent && value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "promote":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" || piece.Type == "queen" {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "clone":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "invisible":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" || piece.Invisible {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare
	}

	return nil
}

func targetSelectionID(mechanic string) string {
	switch mechanic {
	case "freeze":
		return "freeze_target"
	case "sniper", "badsniper":
		return "sniper_target"
	case "demote", "demotehim":
		return "demote_target"
	case "promote", "promotehim":
		return "promote_target"
	case "mindcontrol", "borrow":
		return "mindcontrol_target"
	case "clone":
		return "clone_source"
	case "invisible":
		return "invisible_source"
	case "lavaground":
		return "lavaground_target"
	case "parasite":
		return "parasite_target"
	case "fakepiece":
		return "fakepiece_target"
	default:
		return "target"
	}
}
