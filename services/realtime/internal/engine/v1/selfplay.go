package v1

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// SelfPlayConfig tunes the self-play generation.
type SelfPlayConfig struct {
	Games              int
	MaxPly             int
	SearchDepth        int
	TimePerMove        time.Duration
	TTEntryCount       int
	InitialTemperature float64 // 0 = deterministic, 1 = fully random
	FinalTemperature   float64 // temperature at last game (linear decay)
	RandomizeOpening   bool
	NumThreads         int
}

var DefaultSelfPlayConfig = SelfPlayConfig{
	Games:              500,
	MaxPly:             120,
	SearchDepth:        32,
	TimePerMove:        100 * time.Millisecond,
	TTEntryCount:       1 << 16,
	InitialTemperature: 0.5,
	FinalTemperature:   0.05,
	RandomizeOpening:   true,
}

// genOpening creates a randomized chess position by playing a few random book
// moves from the starting position, so the NNUE sees varied opening structures.
func genOpening(rng *rand.Rand) *contracts.MatchState {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	// Play 2-6 random book moves.
	numMoves := 2 + rng.Intn(5)
	for i := 0; i < numMoves; i++ {
		isWhite := state.Turn == "white"
		moves := generateAllMoves(state, isWhite)
		if len(moves) == 0 {
			break
		}
		move := moves[rng.Intn(len(moves))]
		state = applyMoveCopy(state, &move)
	}
	return state
}

// SelfPlayResult holds the outcome of a self-play game.
type SelfPlayResult struct {
	GameNum   int                `json:"gameNum"`
	PlyCount  int                `json:"plyCount"`
	Result    string             `json:"result"`
	Moves     []string           `json:"moves"`
	FinalFEN  string             `json:"finalFEN"`
	Positions []TrainingPosition `json:"positions"`
}

// TrainingPosition represents a single board position with its TD target.
type TrainingPosition struct {
	FEN       string   `json:"fen"`
	Score     int      `json:"score"`
	Outcome   float64  `json:"outcome"`
	WhiteHand []string `json:"whiteHand,omitempty"`
	BlackHand []string `json:"blackHand,omitempty"`
}

// TrainingDataSet holds all games for export.
type TrainingDataSet struct {
	Config SelfPlayConfig   `json:"config"`
	Games  []SelfPlayResult `json:"games"`
}

// ExportJSON writes all training data to a JSON file.
func (d *TrainingDataSet) ExportJSON(path string) error {
	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal training data: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadTrainingData reads training data from a JSON file.
func LoadTrainingData(path string) (*TrainingDataSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read training data: %w", err)
	}
	var ds TrainingDataSet
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("unmarshal training data: %w", err)
	}
	return &ds, nil
}

// RunSelfPlay runs a batch of self-play games.
func RunSelfPlay(cfg SelfPlayConfig) []SelfPlayResult {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	results := make([]SelfPlayResult, 0, cfg.Games)

	for g := 0; g < cfg.Games; g++ {
		result := playOneGame(g+1, cfg, rng)
		results = append(results, result)
	}

	return results
}

func playOneGame(gameNum int, cfg SelfPlayConfig, rng *rand.Rand) SelfPlayResult {
	// Opening diversity: randomize starting position.
	var state *contracts.MatchState
	if cfg.RandomizeOpening && rng.Float64() < 0.8 {
		state = genOpening(rng)
	} else {
		state = MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	}
	state.WhiteHand = genRandomHand(rng)
	state.BlackHand = genRandomHand(rng)

	// Temperature decays linearly from InitialTemperature to FinalTemperature.
	t := cfg.InitialTemperature
	if cfg.Games > 1 {
		t -= float64(gameNum-1) * (cfg.InitialTemperature - cfg.FinalTemperature) / float64(cfg.Games-1)
		if t < cfg.FinalTemperature {
			t = cfg.FinalTemperature
		}
	}

	tt := NewTranspositionTable(cfg.TTEntryCount)
	var moves []string
	var positions []TrainingPosition

	for ply := 0; ply < cfg.MaxPly; ply++ {
		if state.Status != "active" {
			break
		}
		isWhite := state.Turn == "white"
		genMoves := generateAllMoves(state, isWhite)
		if len(genMoves) == 0 {
			break
		}

		positions = append(positions, TrainingPosition{
			FEN:       boardToSimpleFEN(state),
			Score:     0,
			WhiteHand: handToMechanics(state.WhiteHand),
			BlackHand: handToMechanics(state.BlackHand),
		})

		var bestMove Move

		if t > 0 && rng.Float64() < t {
			bestMove = genMoves[rng.Intn(len(genMoves))]
		} else {
			var result SearchResult
			if cfg.NumThreads > 1 {
				result = ParallelSearch(state, cfg.SearchDepth, tt, cfg.TimePerMove, cfg.NumThreads)
			} else {
				result = SearchWithTime(state, cfg.SearchDepth, tt, cfg.TimePerMove)
			}
			if result.BestMove.From.Row == 0 && result.BestMove.From.Col == 0 &&
				result.BestMove.To.Row == 0 && result.BestMove.To.Col == 0 {
				bestMove = genMoves[0]
			} else {
				bestMove = result.BestMove
				positions[len(positions)-1].Score = result.Score
			}
		}

		moves = append(moves, moveToUCI(&bestMove))
		state = applyMoveCopy(state, &bestMove)
	}

	result := "1/2-1/2"
	outcome := 0.0
	if state.Status == "checkmate" {
		if state.Turn == "white" {
			result = "0-1"
			outcome = -1.0
		} else {
			result = "1-0"
			outcome = 1.0
		}
	}

	// TD(λ) targets propagate the game outcome through all positions.
	// Use raw search scores (not TD targets) for cleaner NNUE training.
	// TD(λ) smoothing is computed in the training script if needed.
	for i := range positions {
		positions[i].Outcome = outcome
	}

	return SelfPlayResult{
		GameNum:   gameNum,
		PlyCount:  len(moves),
		Result:    result,
		Moves:     moves,
		FinalFEN:  boardToSimpleFEN(state),
		Positions: positions,
	}
}

// computeTDTargets computes TD(λ) targets for a trajectory.
func computeTDTargets(positions []TrainingPosition, finalOutcome float64, lam float64) []int {
	n := len(positions)
	targets := make([]int, n)
	if n == 0 {
		return targets
	}
	values := make([]float64, n+1)
	for i, p := range positions {
		values[i] = float64(p.Score) / 100.0
	}
	values[n] = finalOutcome

	for i := n - 1; i >= 0; i-- {
		tdErr := values[i+1] - values[i]
		targets[i] = int((values[i] + tdErr*lam) * 100.0)
	}
	return targets
}

// pickRandomHand returns a list of n random mechanic names.
func genRandomHand(rng *rand.Rand) []contracts.GameCard {
	n := rng.Intn(5) + 1
	names := pickRandomHand(rng, n)
	hand := make([]contracts.GameCard, n)
	for i, name := range names {
		hand[i] = contracts.GameCard{
			ID:       fmt.Sprintf("card-%d", i),
			Name:     name,
			Mechanic: name,
		}
	}
	return hand
}

func pickRandomHand(rng *rand.Rand, n int) []string {
	hand := make([]string, n)
	used := make(map[int]bool)
	for i := 0; i < n; i++ {
		idx := rng.Intn(len(mechanicNames))
		for used[idx] {
			idx = rng.Intn(len(mechanicNames))
		}
		used[idx] = true
		hand[i] = mechanicNames[idx]
	}
	return hand
}

// GenRandomPositions generates N random positions with classical eval for NNUE training.
// Each position is from a random game playthrough with uniform move selection (no search).
func GenRandomPositions(numPositions int) []TrainingPosition {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	positions := make([]TrainingPosition, 0, numPositions)
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")

	for len(positions) < numPositions {
		isWhite := state.Turn == "white"
		moves := generateAllMoves(state, isWhite)

		if len(moves) == 0 || state.Status != "active" {
			state = MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
			continue
		}

		score := ClassicalEval(state.Board, state.Turn, state.LavaSquares, state.FortressZones, state.BombPieces)

		positions = append(positions, TrainingPosition{
			FEN:       boardToSimpleFEN(state),
			Score:     score,
			WhiteHand: pickRandomHand(rng, rng.Intn(5)+1),
			BlackHand: pickRandomHand(rng, rng.Intn(5)+1),
		})

		// Random move for next position.
		move := moves[rng.Intn(len(moves))]
		state = applyMoveCopy(state, &move)

		// Skip every other position to reduce correlation.
		if rng.Intn(2) == 0 && len(positions) < numPositions {
			score2 := ClassicalEval(state.Board, state.Turn, state.LavaSquares, state.FortressZones, state.BombPieces)
			positions = append(positions, TrainingPosition{
				FEN:       boardToSimpleFEN(state),
				Score:     score2,
				WhiteHand: pickRandomHand(rng, rng.Intn(5)+1),
				BlackHand: pickRandomHand(rng, rng.Intn(5)+1),
			})
		}
	}
	return positions
}

// boardToSimpleFEN generates a simple FEN from the state for diagnostic logging.
func BoardToSimpleFEN(state *contracts.MatchState) string {
	return boardToSimpleFEN(state)
}

func handToMechanics(hand []contracts.GameCard) []string {
	names := make([]string, len(hand))
	for i, c := range hand {
		names[i] = c.Mechanic
	}
	return names
}

func boardToSimpleFEN(state *contracts.MatchState) string {
	if state == nil {
		return ""
	}
	pieceChar := func(p *contracts.Piece) byte {
		ch := byte('?')
		switch p.Type {
		case "pawn":
			ch = 'p'
		case "knight":
			ch = 'n'
		case "bishop":
			ch = 'b'
		case "rook":
			ch = 'r'
		case "queen":
			ch = 'q'
		case "king":
			ch = 'k'
		}
		if p.Color == "white" {
			ch -= 32
		}
		return ch
	}
	var fen []byte
	for r := 7; r >= 0; r-- {
		empty := 0
		for c := 0; c < 8; c++ {
			p := state.Board[r][c]
			if p == nil {
				empty++
			} else {
				if empty > 0 {
					fen = append(fen, byte('0'+empty))
					empty = 0
				}
				fen = append(fen, pieceChar(p))
			}
		}
		if empty > 0 {
			fen = append(fen, byte('0'+empty))
		}
		if r > 0 {
			fen = append(fen, '/')
		}
	}
	fen = append(fen, ' ')
	if state.Turn == "white" {
		fen = append(fen, 'w')
	} else {
		fen = append(fen, 'b')
	}
	return string(fen)
}
