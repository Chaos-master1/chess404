package v1

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

type parallelState struct {
	mu        sync.Mutex
	bestMove  Move
	bestScore int
	bestDepth int
	searches  int32
}

func parallelSearch(state *contracts.MatchState, maxDepth int, tt *TranspositionTable, timeLimit time.Duration, startDepth int, ps *parallelState) {
	sc := NewSearchContext(tt, timeLimit)
	turn := state.Turn
	nodes := 0
	prevScore := 0

	for depth := startDepth; depth <= maxDepth; depth++ {
		if depth > 1 && sc.ShouldStop() {
			break
		}

		alpha := math.MinInt + 1
		beta := math.MaxInt - 1
		if depth >= 3 {
			alpha = prevScore - aspirationDelta
			beta = prevScore + aspirationDelta
		}

		score, move := alphaBeta(state, depth, alpha, beta, turn == "white", sc, &nodes, 0)
		if sc.Stopped {
			break
		}
		if score <= alpha || score >= beta {
			score, move = alphaBeta(state, depth, math.MinInt+1, math.MaxInt-1, turn == "white", sc, &nodes, 0)
		}
		if sc.Stopped {
			break
		}

		prevScore = score

		ps.mu.Lock()
		if move != nil && (depth > ps.bestDepth || (depth == ps.bestDepth && score > ps.bestScore)) {
			ps.bestMove = *move
			ps.bestMove.Score = score
			ps.bestScore = score
			ps.bestDepth = depth
		}
		ps.mu.Unlock()

		atomic.AddInt32(&ps.searches, 1)
	}

	sc.Stop()
}

// ParallelSearch runs Lazy SMP parallel search with the given number of threads.
// All threads share the transposition table and converge on the best move.
func ParallelSearch(state *contracts.MatchState, maxDepth int, tt *TranspositionTable, timeLimit time.Duration, numThreads int) SearchResult {
	if numThreads <= 1 {
		return SearchWithTime(state, maxDepth, tt, timeLimit)
	}

	ps := &parallelState{
		bestScore: math.MinInt,
		bestDepth: 0,
	}

	var wg sync.WaitGroup
	for t := 1; t <= numThreads; t++ {
		wg.Add(1)
		go func(threadID int) {
			defer wg.Done()
			// Thread 1 starts from depth 1, others start deeper to diversify early search.
			start := 1
			if threadID > 1 {
				start = threadID
				if start > maxDepth {
					start = maxDepth
				}
			}
			parallelSearch(state, maxDepth, tt, timeLimit, start, ps)
		}(t)
	}
	wg.Wait()

	pv := extractPV(state, tt, state.Turn == "white", 8)

	return SearchResult{
		BestMove: ps.bestMove,
		Score:    ps.bestScore,
		Nodes:    int(ps.searches * 1000),
		Depth:    ps.bestDepth,
		PV:       pv,
	}
}
