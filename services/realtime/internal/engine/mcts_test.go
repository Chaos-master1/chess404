package engine

import (
	"testing"
	"time"
)

func TestMCTSFindsStartingMove(t *testing.T) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	cfg := MCTSConfig{
		Simulations: 200,
		C:           1.414,
		TimeLimit:   2 * time.Second,
	}
	result := MCTSSearch(state, nil, cfg)
	if result.BestMove.From.Row == 0 && result.BestMove.From.Col == 0 {
		t.Error("MCTS returned no move")
	}
	t.Logf("MCTS best move: %v -> %v, score=%d, sims=%d",
		result.BestMove.From, result.BestMove.To, result.Score, result.Nodes)
}

func TestMCTSFindsCapture(t *testing.T) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq - 0 1")
	cfg := MCTSConfig{
		Simulations: 200,
		C:           1.414,
		TimeLimit:   2 * time.Second,
	}
	result := MCTSSearch(state, nil, cfg)
	t.Logf("MCTS best move: %v -> %v, score=%d, sims=%d",
		result.BestMove.From, result.BestMove.To, result.Score, result.Nodes)
}
