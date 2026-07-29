package engine

import (
	"testing"
)

func BenchmarkSearchDepth3(b *testing.B) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	tt := NewTranspositionTable(1 << 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SearchWithTime(state, 3, tt, 5000*1000000) // 5s per move
	}
}

func BenchmarkSearchDepth4(b *testing.B) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	tt := NewTranspositionTable(1 << 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SearchWithTime(state, 4, tt, 10000*1000000)
	}
}

func BenchmarkEvalClassical(b *testing.B) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Evaluate(state.Board, "white", nil, nil)
	}
}
