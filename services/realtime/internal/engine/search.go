package engine

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

const (
	ExactScore  = 0
	LowerBound  = 1
	UpperBound  = 2
)

type TTEntry struct {
	Depth    int
	Score    int
	Flag     int
	BestMove string
}

func (tt *TranspositionTable) Peek(key uint64) (TTEntry, bool) {
	tt.mu.RLock()
	entry, ok := tt.entries[key]
	tt.mu.RUnlock()
	return entry, ok
}

type TranspositionTable struct {
	entries map[uint64]TTEntry
	mu      sync.RWMutex
	maxSize int
}

func NewTranspositionTable(maxSize int) *TranspositionTable {
	return &TranspositionTable{
		entries: make(map[uint64]TTEntry, maxSize),
		maxSize: maxSize,
	}
}

func (tt *TranspositionTable) Lookup(key uint64, depth int, alpha, beta int) (bool, int) {
	tt.mu.RLock()
	entry, ok := tt.entries[key]
	tt.mu.RUnlock()

	if !ok || entry.Depth < depth {
		return false, 0
	}

	if entry.Flag == ExactScore {
		return true, entry.Score
	}
	if entry.Flag == LowerBound && entry.Score >= beta {
		return true, entry.Score
	}
	if entry.Flag == UpperBound && entry.Score <= alpha {
		return true, entry.Score
	}
	return false, 0
}

func (tt *TranspositionTable) GetBestMove(key uint64) string {
	tt.mu.RLock()
	entry, ok := tt.entries[key]
	tt.mu.RUnlock()
	if ok {
		return entry.BestMove
	}
	return ""
}

func (tt *TranspositionTable) Store(key uint64, depth, score, flag int, bestMove string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if len(tt.entries) >= tt.maxSize {
		tt.entries = make(map[uint64]TTEntry, tt.maxSize/2)
	}
	tt.entries[key] = TTEntry{Depth: depth, Score: score, Flag: flag, BestMove: bestMove}
}

type Move struct {
	From      contracts.Square
	To        contracts.Square
	Score     int
	Promotion string // "queen", "rook", "bishop", "knight", or empty for non-promotion
}

type SearchResult struct {
	BestMove Move
	Score    int
	Nodes    int
	Depth    int
	PV       []string // principal variation in UCI notation
}

type SearchContext struct {
	TT       *TranspositionTable
	Nodes    int
	Stopped  bool
	Deadline time.Time
	mu       sync.Mutex
	KillerMoves [64][2]string
	History     [6][64]int
	CounterMoves map[string]string
}

func NewSearchContext(tt *TranspositionTable, timeLimit time.Duration) *SearchContext {
	sc := &SearchContext{
		TT:           tt,
		Deadline:     time.Now().Add(timeLimit),
		CounterMoves: make(map[string]string),
	}
	return sc
}

func (sc *SearchContext) ResetMoveOrdering() {
	for i := range sc.KillerMoves {
		sc.KillerMoves[i] = [2]string{}
	}
	for i := range sc.History {
		for j := range sc.History[i] {
			sc.History[i][j] = 0
		}
	}
	sc.CounterMoves = make(map[string]string)
}

func (sc *SearchContext) ShouldStop() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.Stopped {
		return true
	}
	if time.Now().After(sc.Deadline) {
		sc.Stopped = true
		return true
	}
	return sc.Stopped
}

func (sc *SearchContext) Stop() {
	sc.mu.Lock()
	sc.Stopped = true
	sc.mu.Unlock()
}

var defaultHasher *ZobristHasher

func init() {
	defaultHasher = NewZobristHasher(rand.New(rand.NewSource(0)))
}

func Search(state *contracts.MatchState, maxDepth int, tt *TranspositionTable) SearchResult {
	return SearchWithTime(state, maxDepth, tt, 5*time.Second)
}

func SearchWithTime(state *contracts.MatchState, maxDepth int, tt *TranspositionTable, timeLimit time.Duration) SearchResult {
	turn := state.Turn
	bestMove := Move{}
	nodes := 0
	prevScore := 0
	sc := NewSearchContext(tt, timeLimit)
	sc.Nodes = 0

	searchStart := time.Now()

	for depth := 1; depth <= maxDepth; depth++ {
		if depth > 1 && sc.ShouldStop() {
			break
		}

		alpha := math.MinInt + 1
		beta := math.MaxInt - 1

		if depth >= 3 {
			alpha = prevScore - aspirationDelta
			beta = prevScore + aspirationDelta
		}

		EmitSearchEvent(SearchEvent{
			Type: EventSearchStart, Depth: depth, Nodes: nodes,
		})

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

		if move != nil {
			bestMove = *move
			bestMove.Score = score
		}

		// alphaBeta legitimately returns a nil move at the root -- a
		// mid-iteration stop-check firing before any move is tried, a
		// transposition-table hit short-circuiting move generation entirely,
		// or checkmate/stalemate being detected directly -- and this used to
		// pass that nil straight into MoveToUCI, which dereferences it
		// unconditionally. That crashed the calling goroutine with a nil
		// pointer panic; since the computer opponent runs unrecovered inside
		// match-service's own process, an unlucky position was a path to
		// taking down every match on the instance, not just the one search.
		// Found by the gauntlet harness in TestGauntletDetectsAKnownStrengthGap,
		// which is the first thing to have played this many real, undirected
		// moves against this code.
		moveUCI := ""
		if move != nil {
			moveUCI = MoveToUCI(move)
		}

		pv := []string{moveUCI}
		pvMoves := extractPV(state, tt, turn == "white", 8)
		for _, pm := range pvMoves {
			if pm != "" {
				pv = append(pv, pm)
			}
		}
		EmitSearchEvent(SearchEvent{
			Type: EventDepthDone, Depth: depth, Score: score,
			Move: moveUCI, Nodes: nodes, Pv: pv,
			NPS: int(float64(nodes) / time.Since(searchStart).Seconds()),
		})
	}

	sc.Stop()

	pv := extractPV(state, tt, turn == "white", 8)

	EmitSearchEvent(SearchEvent{
		Type: EventBestMove, Move: MoveToUCI(&bestMove),
		Score: bestMove.Score, Depth: maxDepth, Nodes: nodes,
	})

	return SearchResult{
		BestMove: bestMove,
		Score:    bestMove.Score,
		Nodes:    nodes,
		Depth:    maxDepth,
		PV:       pv,
	}
}

const (
	lmrMinDepth      = 3
	lmrReduction     = 1
	nullMoveDepth    = 4
	nullMoveR        = 2
	aspirationDelta  = 50
	checkExtension   = 1
	razorDepth       = 3
	razorMargin      = 300
	futilityDepth    = 3
	futilityMargin   = 200
	iidDepthReduce   = 2
	iidMinDepth      = 4
)

func alphaBeta(state *contracts.MatchState, depth, alpha, beta int, maximizing bool, sc *SearchContext, nodes *int, ply int) (int, *Move) {
	*nodes++

	if *nodes&8191 == 0 && sc.ShouldStop() {
		return 0, nil
	}

	hash := defaultHasher.Hash(state)

	// Draw detection
	if ply > 0 {
		if state.HalfMoveClock >= 100 {
			return 0, nil
		}
		if isInsufficientMaterial(state.Board) {
			return 0, nil
		}
	}

	ttBestMove := ""
	if sc.TT != nil {
		if ok, ttScore := sc.TT.Lookup(hash, depth, alpha, beta); ok {
			return ttScore, nil
		}
		ttBestMove = sc.TT.GetBestMove(hash)
	}

	if depth <= 0 {
		return quiescence(state, alpha, beta, maximizing, sc, nodes, ply, hash), nil
	}

	// Razoring: at shallow depths, if the static eval is far below alpha, drop to
	// quiescence. Avoids searching hopeless positions.
		if depth <= razorDepth && !isKingInCheck(state) {
			staticEval := EvaluateWithModifiers(state.Board, state.Turn, state.LavaSquares, state.FortressZones, state.BombPieces, state.WhiteHand, state.BlackHand)
			if !maximizing {
				staticEval = -staticEval
			}
			margin := razorMargin + 50*(razorDepth-depth)
			if staticEval+margin < alpha {
				qScore := quiescence(state, alpha, beta, maximizing, sc, nodes, ply, hash)
				if qScore < alpha+50 {
					return qScore, nil
				}
			}
		}

	if ply > 0 && depth >= nullMoveDepth && !isKingInCheck(state) {
		nullState := cloneMatchState(state)
		nullState.Turn = oppositeColor(state.Turn)
		nullScore, _ := alphaBeta(nullState, depth-nullMoveR-1, -beta, -beta+1, !maximizing, sc, nodes, ply+1)
		nullScore = -nullScore
		if nullScore >= beta {
			return beta, nil
		}
	}

	inCheck := isKingInCheck(state)

	// Internal Iterative Deepening: when depth is high and we have no TT move,
	// search at reduced depth first to get a better move ordering.
	iidSearch := false
	if depth >= iidMinDepth && sc.TT != nil && ttBestMove == "" {
		iidSearch = true
	}

	moves := generateAllMoves(state, maximizing)
	if len(moves) == 0 {
		if inCheck {
			if maximizing {
				return -20000 + ply, nil
			}
			return 20000 - ply, nil
		}
		return 0, nil
	}

	if iidSearch && len(moves) > 1 {
		_, iidMove := alphaBeta(state, depth-iidDepthReduce, alpha, beta, maximizing, sc, nodes, ply)
		if iidMove != nil {
			// Move the IID best move to front.
			for i := range moves {
				if moves[i].From == iidMove.From && moves[i].To == iidMove.To {
					moves[i].Score += 200
					break
				}
			}
		}
	}

	orderMoves(sc, moves, state, ply, ttBestMove)

	bestMove := &moves[0]
	improving := true

	if maximizing {
		maxEval := math.MinInt + 1
		for i := range moves {
			if sc.Stopped {
				break
			}

			newDepth := depth - 1

			isCheck := false
			if checkExtension > 0 {
				if piece := state.Board[moves[i].From.Row][moves[i].From.Col]; piece != nil {
					testState := applyMoveCopy(state, &moves[i])
					oppKing := findKingPos(testState.Board, oppositeColor(piece.Color))
					if oppKing != nil && isAttacked(testState.Board, *oppKing, piece.Color) {
						isCheck = true
					}
				}
				if isCheck {
					newDepth += checkExtension
				}
			}

			captured := state.Board[moves[i].To.Row][moves[i].To.Col]
			isCapture := captured != nil
			isPromo := moves[i].Promotion != ""

			// Futility pruning: at shallow depths, prune quiet moves unlikely to improve alpha.
			if i > 0 && depth <= futilityDepth && !isCapture && !isPromo && !isCheck && !inCheck {
				if maxEval+futilityMargin <= alpha {
					continue
				}
			}

			if i >= 3 && depth >= lmrMinDepth && !isCapture && !isPromo && !isCheck {
				reduction := 1
				if depth >= 6 {
					reduction = 2
				}
				if i >= 8 {
					reduction++
				}
				if !improving {
					reduction++
				}
				reduced := depth - 1 - reduction
				if reduced < 1 {
					reduced = 1
				}
				newDepth = reduced
			}

			newState := applyMoveCopy(state, &moves[i])

			var eval int
			if i == 0 {
				eval, _ = alphaBeta(newState, newDepth, alpha, beta, false, sc, nodes, ply+1)
			} else {
				// Null-window search: test if move can beat alpha.
				eval, _ = alphaBeta(newState, newDepth, alpha, alpha+1, false, sc, nodes, ply+1)
				if eval > alpha {
					// Re-search at full depth with null window.
					eval, _ = alphaBeta(newState, depth-1, alpha, alpha+1, false, sc, nodes, ply+1)
					if eval > alpha && eval < beta {
						// Re-search with full window (new PV candidate).
						eval, _ = alphaBeta(newState, depth-1, alpha, beta, false, sc, nodes, ply+1)
					}
				}
			}

		if eval > maxEval {
			maxEval = eval
			bestMove = &moves[i]
		}
		alpha = max(alpha, eval)
		if beta <= alpha {
				storeKillerMove(sc, ply, keyForSquare(moves[i].From)+keyForSquare(moves[i].To))
				storeCounterMove(sc, state, &moves[i])
				attacker := state.Board[moves[i].From.Row][moves[i].From.Col]
				if attacker != nil && !isCapture {
					updateHistory(sc, attacker.Type, moves[i].To.Row*8+moves[i].To.Col, depth)
				}
				break
			}
		}
		if sc.TT != nil {
			flag := ExactScore
			if maxEval <= alpha {
				flag = UpperBound
			} else if maxEval >= beta {
				flag = LowerBound
			}
			sc.TT.Store(hash, depth, maxEval, flag, keyForSquare(bestMove.From)+keyForSquare(bestMove.To))
		}
	return maxEval, bestMove
	}

	minEval := math.MaxInt - 1
	for i := range moves {
		if sc.Stopped {
			break
		}

		newDepth := depth - 1

		isCheck := false
		if checkExtension > 0 {
			if piece := state.Board[moves[i].From.Row][moves[i].From.Col]; piece != nil {
				testState := applyMoveCopy(state, &moves[i])
				oppKing := findKingPos(testState.Board, oppositeColor(piece.Color))
				if oppKing != nil && isAttacked(testState.Board, *oppKing, piece.Color) {
					isCheck = true
				}
			}
			if isCheck {
				newDepth += checkExtension
			}
		}

		captured := state.Board[moves[i].To.Row][moves[i].To.Col]
		isCapture := captured != nil
		isPromo := moves[i].Promotion != ""

		if i > 0 && depth <= futilityDepth && !isCapture && !isPromo && !isCheck && !inCheck {
			if minEval-futilityMargin >= beta {
				continue
			}
		}

		if i >= 3 && depth >= lmrMinDepth && !isCapture && !isPromo && !isCheck {
			reduction := 1
			if depth >= 6 {
				reduction = 2
			}
			if i >= 8 {
				reduction++
			}
			if !improving {
				reduction++
			}
			reduced := depth - 1 - reduction
			if reduced < 1 {
				reduced = 1
			}
			newDepth = reduced
		}

		newState := applyMoveCopy(state, &moves[i])

		var eval int
		if i == 0 {
			eval, _ = alphaBeta(newState, newDepth, alpha, beta, true, sc, nodes, ply+1)
		} else {
			eval, _ = alphaBeta(newState, newDepth, beta-1, beta, true, sc, nodes, ply+1)
			if eval < beta {
				eval, _ = alphaBeta(newState, depth-1, beta-1, beta, true, sc, nodes, ply+1)
				if eval < beta {
					eval, _ = alphaBeta(newState, depth-1, alpha, beta, true, sc, nodes, ply+1)
				}
			}
		}

		if eval < minEval {
			minEval = eval
			bestMove = &moves[i]
		}
		beta = min(beta, eval)
		if beta <= alpha {
			storeKillerMove(sc, ply, keyForSquare(moves[i].From)+keyForSquare(moves[i].To))
			storeCounterMove(sc, state, &moves[i])
			attacker := state.Board[moves[i].From.Row][moves[i].From.Col]
			if attacker != nil && !isCapture {
				updateHistory(sc, attacker.Type, moves[i].To.Row*8+moves[i].To.Col, depth)
			}
			break
		}
	}
		if sc.TT != nil {
			flag := ExactScore
			if minEval <= alpha {
				flag = UpperBound
			} else if minEval >= beta {
				flag = LowerBound
			}
			sc.TT.Store(hash, depth, minEval, flag, keyForSquare(bestMove.From)+keyForSquare(bestMove.To))
		}
		return minEval, bestMove
}

// quiescence searches captures at depth 0 (stand-pat) to reduce the horizon
// effect. Delta pruning filters losing captures. Returns a score from the
// perspective of the side to move.
func quiescence(state *contracts.MatchState, alpha, beta int, maximizing bool, sc *SearchContext, nodes *int, ply int, hash uint64) int {
	*nodes++

	if *nodes&16383 == 0 && sc.ShouldStop() {
		return 0
	}

	standPat := EvaluateWithModifiers(state.Board, state.Turn, state.LavaSquares, state.FortressZones, state.BombPieces, state.WhiteHand, state.BlackHand)
	if !maximizing {
		standPat = -standPat
	}

	if standPat >= beta {
		return beta
	}
	if standPat > alpha {
		alpha = standPat
	}

	const deltaMargin = 200
	captures := generateCaptureMoves(state, maximizing)
	for i := range captures {
		if sc.Stopped {
			break
		}

		captured := state.Board[captures[i].To.Row][captures[i].To.Col]
		capturedValue := 0
		if captured != nil {
			capturedValue = pieceValue(captured.Type)
		}

		// Delta pruning: if stand-pat + captured value + margin can't reach alpha, skip.
		if standPat+capturedValue+deltaMargin < alpha {
			continue
		}

		// SEE pruning: if the capture loses material, skip it in quiescence.
		seeScore := see(state, &captures[i])
		if seeScore < 0 {
			continue
		}

		newState := applyMoveCopy(state, &captures[i])
		score := -quiescence(newState, -beta, -alpha, !maximizing, sc, nodes, ply+1, 0)
		if score >= beta {
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}

	return alpha
}

// see (Static Exchange Evaluation) determines the net material gain from
// a capture sequence at the target square. Returns the estimated score
// from the perspective of the side initiating the capture.
func see(state *contracts.MatchState, move *Move) int {
	fromPiece := state.Board[move.From.Row][move.From.Col]
	toPiece := state.Board[move.To.Row][move.To.Col]
	if fromPiece == nil {
		return 0
	}

	targetSquare := move.To
	// Simulate the exchange on the target square.
	// We alternate between sides, always capturing with the least valuable piece.
	sq := targetSquare
	side := fromPiece.Color
	value := 0
	if toPiece != nil {
		value = pieceValue(toPiece.Type)
		if toPiece.FusedWith != "" {
			value = (value + pieceValue(toPiece.FusedWith)) / 2
		}
	}

	// First capture by the moving side.
	gain := value
	// Remove the target piece.
	attackerVal := pieceValue(fromPiece.Type)
	if fromPiece.FusedWith != "" {
		attackerVal = (attackerVal + pieceValue(fromPiece.FusedWith)) / 2
	}

	// Now check if the opponent can recapture on the same square.
	opponent := oppositeColor(side)
	// Get the least valuable opponent attacker.
	for ply := 0; ply < 8; ply++ {
		lva := leastValuableAttacker(state.Board, sq, opponent)
		if lva == nil {
			break
		}
		lvaVal := pieceValue(lva.Type)
		if lva.FusedWith != "" {
			lvaVal = (lvaVal + pieceValue(lva.FusedWith)) / 2
		}
		gain = -gain + lvaVal
		opponent = side
		side = oppositeColor(opponent)
	}
	return gain
}

// leastValuableAttacker finds the least valuable piece of the given color attacking the square.
func leastValuableAttacker(board [][]*contracts.Piece, sq contracts.Square, color string) *contracts.Piece {
	order := []string{"pawn", "knight", "bishop", "rook", "queen", "king"}
	for _, ptype := range order {
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := board[r][c]
				if piece == nil || piece.Color != color || piece.Type != ptype {
					continue
				}
				from := contracts.Square{Row: r, Col: c}
				if pseudoAttacks(board, from, sq, piece) {
					return piece
				}
			}
		}
	}
	return nil
}

// pseudoAttacks checks if a piece at 'from' can pseudo-legal attack 'to'.
func pseudoAttacks(board [][]*contracts.Piece, from, to contracts.Square, piece *contracts.Piece) bool {
	dr := to.Row - from.Row
	dc := to.Col - from.Col
	switch piece.Type {
	case "pawn":
		push := -1
		if piece.Color == "black" {
			push = 1
		}
		return dr == push && (dc == -1 || dc == 1)
	case "knight":
		return (dr*dr == 4 && dc*dc == 1) || (dr*dr == 1 && dc*dc == 4)
	case "bishop":
		if dr*dr != dc*dc {
			return false
		}
		return rayClear(board, from, to)
	case "rook":
		if dr != 0 && dc != 0 {
			return false
		}
		return rayClear(board, from, to)
	case "queen":
		if dr*dr != dc*dc && dr != 0 && dc != 0 {
			return false
		}
		return rayClear(board, from, to)
	case "king":
		return dr*dr <= 1 && dc*dc <= 1 && (dr != 0 || dc != 0)
	}
	return false
}

// rayClear checks if the ray from -> to is unobstructed.
func rayClear(board [][]*contracts.Piece, from, to contracts.Square) bool {
	dr := to.Row - from.Row
	dc := to.Col - from.Col
	if dr != 0 {
		dr /= abs(dr)
	}
	if dc != 0 {
		dc /= abs(dc)
	}
	r, c := from.Row+dr, from.Col+dc
	for r != to.Row || c != to.Col {
		if board[r][c] != nil {
			return false
		}
		r += dr
		c += dc
	}
	return true
}

// generateCaptureMoves returns only moves that capture an enemy piece.
func generateCaptureMoves(state *contracts.MatchState, forWhite bool) []Move {
	color := "black"
	if forWhite {
		color = "white"
	}

	var captures []Move
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece == nil || piece.Color != color || piece.Frozen {
				continue
			}
			from := contracts.Square{Row: r, Col: c}
			candidates := legalMovesWithFusion(state.Board, from, state.LastMove, sliceToSet(state.Moved))
			for _, to := range candidates {
				if fortressEntryBlocked(state.FortressZones, piece.Color, to) {
					continue
				}
				target := state.Board[to.Row][to.Col]
				if target == nil || target.Color == color {
					continue
				}
				if piece.Type == "pawn" && (to.Row == 0 || to.Row == 7) {
					for _, promo := range []string{"queen", "rook", "bishop", "knight"} {
						captures = append(captures, Move{From: from, To: to, Promotion: promo})
					}
				} else {
					captures = append(captures, Move{From: from, To: to})
				}
			}
		}
	}
	return captures
}

func GenerateAllMoves(state *contracts.MatchState, forWhite bool) []Move {
	return generateAllMoves(state, forWhite)
}

func generateAllMoves(state *contracts.MatchState, forWhite bool) []Move {
	color := "black"
	if forWhite {
		color = "white"
	}

	if state.DoubleMove != nil && state.DoubleMove.MovesLeft > 0 && state.DoubleMove.TrackedSq != nil && state.DoubleMove.Type == "same" {
		tracked := state.DoubleMove.TrackedSq
		piece := state.Board[tracked.Row][tracked.Col]
		if piece != nil && piece.Color == color && !piece.Frozen {
			candidates := legalMovesWithFusion(state.Board, *tracked, state.LastMove, sliceToSet(state.Moved))
			var moves []Move
			for _, to := range candidates {
				if fortressEntryBlocked(state.FortressZones, piece.Color, to) {
					continue
				}
				if piece.Type == "pawn" && (to.Row == 0 || to.Row == 7) {
					for _, promo := range []string{"queen", "rook", "bishop", "knight"} {
						moves = append(moves, Move{From: *tracked, To: to, Promotion: promo})
					}
				} else {
					moves = append(moves, Move{From: *tracked, To: to})
				}
			}
			if state.InvisiblePiece != nil && state.InvisiblePiece.OwnerColor == color && state.InvisiblePiece.RoundsLeft > 0 && !state.InvisiblePiece.Piece.Frozen {
				from := contracts.Square{Row: state.InvisiblePiece.Row, Col: state.InvisiblePiece.Col}
				ghostCandidates := legalMovesWithFusion(state.Board, from, state.LastMove, sliceToSet(state.Moved))
				for _, to := range ghostCandidates {
					if fortressEntryBlocked(state.FortressZones, color, to) {
						continue
					}
					if state.InvisiblePiece.Piece.Type == "pawn" && (to.Row == 0 || to.Row == 7) {
						for _, promo := range []string{"queen", "rook", "bishop", "knight"} {
							moves = append(moves, Move{From: from, To: to, Promotion: promo})
						}
					} else {
						moves = append(moves, Move{From: from, To: to})
					}
				}
			}
			return moves
		}
		return nil
	}

	var moves []Move
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := state.Board[r][c]
			if piece == nil || piece.Color != color {
				continue
			}
			if piece.Frozen {
				continue
			}
			from := contracts.Square{Row: r, Col: c}

			// Double-move "diff": skip the tracked square (only other pieces may move).
			if state.DoubleMove != nil && state.DoubleMove.MovesLeft > 0 &&
				state.DoubleMove.TrackedSq != nil && state.DoubleMove.Type == "diff" {
				if from.Row == state.DoubleMove.TrackedSq.Row && from.Col == state.DoubleMove.TrackedSq.Col {
					continue
				}
			}

			candidates := legalMovesWithFusion(state.Board, from, state.LastMove, sliceToSet(state.Moved))

			for _, to := range candidates {
				if fortressEntryBlocked(state.FortressZones, piece.Color, to) {
					continue
				}
				if piece.Type == "pawn" && (to.Row == 0 || to.Row == 7) {
					for _, promo := range []string{"queen", "rook", "bishop", "knight"} {
						moves = append(moves, Move{From: from, To: to, Promotion: promo})
					}
				} else {
					moves = append(moves, Move{From: from, To: to})
				}
			}
		}
	}

	// Generate moves for the invisible piece if it belongs to the current player.
	if state.InvisiblePiece != nil && state.InvisiblePiece.OwnerColor == color && state.InvisiblePiece.RoundsLeft > 0 {
		from := contracts.Square{Row: state.InvisiblePiece.Row, Col: state.InvisiblePiece.Col}
		invisiblePiece := state.InvisiblePiece.Piece
		if !invisiblePiece.Frozen {
			candidates := legalMovesWithFusion(state.Board, from, state.LastMove, sliceToSet(state.Moved))
			for _, to := range candidates {
				if fortressEntryBlocked(state.FortressZones, color, to) {
					continue
				}
				if invisiblePiece.Type == "pawn" && (to.Row == 0 || to.Row == 7) {
					for _, promo := range []string{"queen", "rook", "bishop", "knight"} {
						moves = append(moves, Move{From: from, To: to, Promotion: promo})
					}
				} else {
					moves = append(moves, Move{From: from, To: to})
				}
			}
		}
	}

	return moves
}

// Move ordering tables are now on SearchContext for thread safety.

// counterMoveKey builds a key from the last move for countermove lookup.
func counterMoveKey(state *contracts.MatchState) string {
	if state.LastMove == nil {
		return ""
	}
	from := state.LastMove.From
	to := state.LastMove.To
	// Use from/to of the last move as the key.
	return fmt.Sprintf("%d.%d.%d.%d", from.Row, from.Col, to.Row, to.Col)
}

// storeCounterMove records a counter move that caused a beta cutoff.
func storeCounterMove(sc *SearchContext, state *contracts.MatchState, move *Move) {
	key := counterMoveKey(state)
	if key == "" {
		return
	}
	moveKey := keyForSquare(move.From) + keyForSquare(move.To)
	sc.CounterMoves[key] = moveKey
}

func pieceTypeIndex(pieceType string) int {
	switch pieceType {
	case "pawn":
		return 0
	case "knight":
		return 1
	case "bishop":
		return 2
	case "rook":
		return 3
	case "queen":
		return 4
	case "king":
		return 5
	}
	return 0
}

func orderMoves(sc *SearchContext, moves []Move, state *contracts.MatchState, ply int, ttBestMove string) {
	ttKey := ""
	_ = ttKey
	for i := range moves {
		score := 0
		// TT best move gets the highest priority.
		if ttBestMove != "" {
			key := keyForSquare(moves[i].From) + keyForSquare(moves[i].To)
			if key == ttBestMove {
				ttKey = key
				score += 10000
			}
		}
		captured := state.Board[moves[i].To.Row][moves[i].To.Col]
		if captured != nil {
			seeScore := see(state, &moves[i])
			if seeScore >= 0 {
				score += 1000 + seeScore
			} else {
				score -= 500 - seeScore
			}
		}
		if moves[i].Score > 0 {
			score += 100
		}
		if moves[i].To.Row == 3 || moves[i].To.Row == 4 {
			score += 10
		}
		if moves[i].Promotion != "" {
			if moves[i].Promotion == "queen" {
				score += 900
			} else {
				score += 200
			}
		}
		moves[i].Score = score
	}

	for i := range moves {
		key := keyForSquare(moves[i].From) + keyForSquare(moves[i].To)
		// Skip TT best move — already scored highest.
		if key == ttKey {
			continue
		}
		kp := ply
		if kp >= len(sc.KillerMoves) {
			kp = 0
		}
		for _, k := range sc.KillerMoves[kp] {
			if k == key && k != "" {
				moves[i].Score += 500
				break
			}
		}
		attacker := state.Board[moves[i].From.Row][moves[i].From.Col]
		if attacker != nil {
			idx := pieceTypeIndex(attacker.Type)
			moves[i].Score += sc.History[idx][moves[i].To.Row*8+moves[i].To.Col]
		}
		// Counter move bonus.
		cmKey := counterMoveKey(state)
		if cmKey != "" {
			if cm, ok := sc.CounterMoves[cmKey]; ok && cm == key {
				moves[i].Score += 400
			}
		}
	}

	sort.SliceStable(moves, func(i, j int) bool {
		return moves[i].Score > moves[j].Score
	})
}

// storeKillerMove records a move that caused a beta cutoff at the given ply.
func storeKillerMove(sc *SearchContext, ply int, moveKey string) {
	if ply >= len(sc.KillerMoves) {
		return
	}
	// Shift and store in the first slot (most recent).
	sc.KillerMoves[ply][1] = sc.KillerMoves[ply][0]
	sc.KillerMoves[ply][0] = moveKey
}

// updateHistory increments the history counter for a move that caused a cutoff.
func updateHistory(sc *SearchContext, pieceType string, toSquare int, depth int) {
	idx := pieceTypeIndex(pieceType)
	sc.History[idx][toSquare] += depth * depth
}

func ApplyMoveCopy(state *contracts.MatchState, move *Move) *contracts.MatchState {
	return applyMoveCopy(state, move)
}

func applyMoveCopy(state *contracts.MatchState, move *Move) *contracts.MatchState {
	newState := cloneMatchState(state)
	piece := newState.Board[move.From.Row][move.From.Col]
	if piece == nil {
		return newState
	}

	captured := newState.Board[move.To.Row][move.To.Col]
	newState.Board[move.To.Row][move.To.Col] = piece
	newState.Board[move.From.Row][move.From.Col] = nil

	if piece.Type == "pawn" && move.From.Col != move.To.Col && captured == nil {
		newState.Board[move.From.Row][move.To.Col] = nil
	}

	if piece.Type == "king" && abs(move.To.Col-move.From.Col) == 2 {
		if move.To.Col == 6 {
			newState.Board[move.From.Row][5] = newState.Board[move.From.Row][7]
			newState.Board[move.From.Row][7] = nil
			newState.Moved = append(newState.Moved, keyForCoords(move.From.Row, 7))
		} else if move.To.Col == 2 {
			newState.Board[move.From.Row][3] = newState.Board[move.From.Row][0]
			newState.Board[move.From.Row][0] = nil
			newState.Moved = append(newState.Moved, keyForCoords(move.From.Row, 0))
		}
	}

	if piece.Type == "pawn" && (move.To.Row == 0 || move.To.Row == 7) {
		promoType := move.Promotion
		if promoType == "" {
			promoType = "queen"
		}
		newState.Board[move.To.Row][move.To.Col] = &contracts.Piece{
			Type:  promoType,
			Color: piece.Color,
		}
	}

	justMovedColor := newState.Turn
	newState.Turn = oppositeColor(piece.Color)
	newState.Moved = append(newState.Moved, keyForSquare(move.From))
	newState.LastMove = &contracts.LastMove{From: move.From, To: move.To}

	if newState.DoubleMove != nil {
		newMovesLeft := newState.DoubleMove.MovesLeft - 1
		if newMovesLeft > 0 {
			tracked := contracts.Square{Row: move.To.Row, Col: move.To.Col}
			newState.DoubleMove = &contracts.DoubleMoveState{
				Type:      newState.DoubleMove.Type,
				MovesLeft: newMovesLeft,
				TrackedSq: &tracked,
			}
		} else {
			newState.DoubleMove = nil
		}
	}

	if newState.InvisiblePiece != nil && newState.InvisiblePiece.OwnerColor == justMovedColor {
		from := contracts.Square{Row: newState.InvisiblePiece.Row, Col: newState.InvisiblePiece.Col}
		if from.Row == move.From.Row && from.Col == move.From.Col {
			ghostBoard := cloneBoard(newState.Board)
			ghostBoard[move.To.Row][move.To.Col] = &contracts.Piece{
				Type:  newState.InvisiblePiece.Piece.Type,
				Color: newState.InvisiblePiece.Piece.Color,
			}
			oppKing := findKingPos(ghostBoard, newState.Turn)
			givesCheck := oppKing != nil && isAttacked(ghostBoard, *oppKing, justMovedColor)
			isMove2 := newState.InvisiblePiece.RoundsLeft <= 0
			if givesCheck || isMove2 {
				newState.InvisiblePiece = nil
			} else {
				newState.InvisiblePiece.Row = move.To.Row
				newState.InvisiblePiece.Col = move.To.Col
				newState.InvisiblePiece.RoundsLeft--
			}
		} else {
			if newState.InvisiblePiece.RoundsLeft > 0 {
				newState.InvisiblePiece.RoundsLeft--
			}
			if newState.InvisiblePiece.RoundsLeft <= 0 {
				newState.InvisiblePiece = nil
			}
		}
	} else if newState.InvisiblePiece != nil && newState.InvisiblePiece.OwnerColor != justMovedColor {
		if newState.InvisiblePiece.RoundsLeft > 0 {
			newState.InvisiblePiece.RoundsLeft--
		}
		if newState.InvisiblePiece.RoundsLeft <= 0 {
			newState.InvisiblePiece = nil
		}
	}

	return newState
}

func IsKingInCheck(state *contracts.MatchState) bool {
	return isKingInCheck(state)
}

// extractPV follows the TT best moves from the root to build a principal variation.
func extractPV(state *contracts.MatchState, tt *TranspositionTable, maximizing bool, maxLen int) []string {
	if tt == nil {
		return nil
	}
	pv := make([]string, 0, maxLen)
	cur := state
	for i := 0; i < maxLen; i++ {
		hash := defaultHasher.Hash(cur)
		moveStr := tt.GetBestMove(hash)
		if moveStr == "" {
			break
		}
		// Parse the best move string (e.g., "e2e4") and apply it.
		move := parseUCIMove(cur, moveStr, maximizing)
		if move == nil {
			break
		}
		pv = append(pv, moveStr)
		cur = applyMoveCopy(cur, move)
		maximizing = !maximizing
	}
	return pv
}

// parseUCIMove converts a UCI string like "e2e4" or "e7e8q" into a Move.
func parseUCIMove(state *contracts.MatchState, uci string, maximizing bool) *Move {
	if len(uci) < 4 {
		return nil
	}
	fromFile := int(uci[0] - 'a')
	fromRank := int(uci[1] - '1')
	toFile := int(uci[2] - 'a')
	toRank := int(uci[3] - '1')
	if fromFile < 0 || fromFile > 7 || fromRank < 0 || fromRank > 7 ||
		toFile < 0 || toFile > 7 || toRank < 0 || toRank > 7 {
		return nil
	}
	move := &Move{
		From: contracts.Square{Row: fromRank, Col: fromFile},
		To:   contracts.Square{Row: toRank, Col: toFile},
	}
	if len(uci) >= 5 {
		promo := uci[4]
		switch promo {
		case 'q':
			move.Promotion = "queen"
		case 'r':
			move.Promotion = "rook"
		case 'b':
			move.Promotion = "bishop"
		case 'n':
			move.Promotion = "knight"
		}
	}
	return move
}

func isKingInCheck(state *contracts.MatchState) bool {
	king := findKingPos(state.Board, state.Turn)
	if king == nil {
		return false
	}
	return isAttackedWithFusion(state.Board, *king, oppositeColor(state.Turn))
}

func isInsufficientMaterial(board [][]*contracts.Piece) bool {
	whitePieces := 0
	blackPieces := 0
	whiteBishopSq := -1
	blackBishopSq := -1
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			piece := board[r][c]
			if piece == nil || piece.Type == "king" {
				continue
			}
			if piece.Color == "white" {
				whitePieces++
				if piece.Type == "bishop" {
					whiteBishopSq = r*8 + c
				}
			} else {
				blackPieces++
				if piece.Type == "bishop" {
					blackBishopSq = r*8 + c
				}
			}
		}
	}
	// K vs K
	if whitePieces == 0 && blackPieces == 0 {
		return true
	}
	// K+B vs K or K+N vs K
	if whitePieces == 1 && blackPieces == 0 {
		return true
	}
	if whitePieces == 0 && blackPieces == 1 {
		return true
	}
	// K+B vs K+B with same-colored bishops
	if whitePieces == 1 && blackPieces == 1 {
		if whiteBishopSq >= 0 && blackBishopSq >= 0 {
			if whiteBishopSq%2 == blackBishopSq%2 {
				return true
			}
		}
	}
	return false
}

func cloneMatchState(state *contracts.MatchState) *contracts.MatchState {
	newState := &contracts.MatchState{
		MatchID:     state.MatchID,
		Turn:        state.Turn,
		Status:      state.Status,
		HalfMoveClock: state.HalfMoveClock,
		FullMoveNum: state.FullMoveNum,
		WhiteHand:   append([]contracts.GameCard{}, state.WhiteHand...),
		BlackHand:   append([]contracts.GameCard{}, state.BlackHand...),
		Moved:       append([]string{}, state.Moved...),
		LastMove:    state.LastMove,
	}
	newState.Board = cloneBoard(state.Board)
	if state.DoubleMove != nil {
		dm := *state.DoubleMove
		if state.DoubleMove.TrackedSq != nil {
			tracked := *state.DoubleMove.TrackedSq
			dm.TrackedSq = &tracked
		}
		newState.DoubleMove = &dm
	}
	if state.InvisiblePiece != nil {
		ip := *state.InvisiblePiece
		newState.InvisiblePiece = &ip
	}
	return newState
}

func cloneBoard(board [][]*contracts.Piece) [][]*contracts.Piece {
	newBoard := make([][]*contracts.Piece, 8)
	for r := 0; r < 8; r++ {
		newBoard[r] = make([]*contracts.Piece, 8)
		for c := 0; c < 8; c++ {
			if board[r][c] != nil {
				pieceCopy := *board[r][c]
				newBoard[r][c] = &pieceCopy
			}
		}
	}
	return newBoard
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func oppositeColor(color string) string {
	if color == "white" {
		return "black"
	}
	return "white"
}

func keyForSquare(sq contracts.Square) string {
	return keyForCoords(sq.Row, sq.Col)
}

func keyForCoords(row, col int) string {
	return fmt.Sprintf("%d-%d", row, col)
}

func sliceToSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}
