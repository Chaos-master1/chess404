package v1

import (
	"math"
	"sort"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

type MCTSConfig struct {
	Simulations int
	C           float64
	TimeLimit   time.Duration
}

var DefaultMCTSConfig = MCTSConfig{
	Simulations: 800,
	C:           1.414,
	TimeLimit:   200 * time.Millisecond,
}

type MCTSNode struct {
	state      *contracts.MatchState
	parent     *MCTSNode
	children   []*MCTSNode
	move       *Move
	visits     int
	valueSum   float64
	prior      float64
	untried    []Move
	maximizing bool
}

func NewMCTSNode(state *contracts.MatchState, parent *MCTSNode, move *Move, prior float64) *MCTSNode {
	node := &MCTSNode{
		state:      state,
		parent:     parent,
		move:       move,
		prior:      prior,
		maximizing: state.Turn == "white",
	}
	if state.Status == "active" {
		node.untried = generateAllMoves(state, state.Turn == "white")
	}
	return node
}

func (n *MCTSNode) ucb(c float64, parentVisits int) float64 {
	if n.visits == 0 {
		return math.MaxFloat64
	}
	q := n.valueSum / float64(n.visits)
	explore := c * n.prior * math.Sqrt(float64(parentVisits)) / (1.0 + float64(n.visits))
	return q + explore
}

func (n *MCTSNode) selectChild(c float64) *MCTSNode {
	best := n.children[0]
	bestVal := best.ucb(c, n.visits)
	for i := 1; i < len(n.children); i++ {
		val := n.children[i].ucb(c, n.visits)
		if val > bestVal {
			bestVal = val
			best = n.children[i]
		}
	}
	return best
}

func MCTSSearch(rootState *contracts.MatchState, tt *TranspositionTable, cfg MCTSConfig) SearchResult {
	root := NewMCTSNode(rootState, nil, nil, 1.0)
	deadline := time.Now().Add(cfg.TimeLimit)
	sims := 0

	for sims < cfg.Simulations {
		if time.Now().After(deadline) {
			break
		}
		sims++

		node := root
		path := []*MCTSNode{node}
		for len(node.untried) == 0 && len(node.children) > 0 {
			node = node.selectChild(cfg.C)
			path = append(path, node)
		}

		if len(node.untried) > 0 && node.state.Status == "active" {
			move := node.untried[0]
			node.untried = node.untried[1:]

			ttKey := simpleHash(node.state)
			ttBest := ""
			if tt != nil {
				ttBest = tt.GetBestMove(ttKey)
			}
			prior := 0.5
			if ttBest != "" && moveKey(&move) == ttBest {
				prior = 0.9
			} else if node.state.Board[move.To.Row][move.To.Col] != nil {
				prior = 0.7
			} else if move.Promotion != "" {
				prior = 0.6
			}

			newState := applyMoveCopy(node.state, &move)
			child := NewMCTSNode(newState, node, &move, prior)
			node.children = append(node.children, child)
			node = child
			path = append(path, node)
		}

		value := evaluateMCTSValue(node.state)

		for i := len(path) - 1; i >= 0; i-- {
			path[i].visits++
			path[i].valueSum += value
			value = -value
		}
	}

	if len(root.children) == 0 {
		return SearchResult{}
	}

	sort.Slice(root.children, func(i, j int) bool {
		return root.children[i].visits > root.children[j].visits
	})

	best := root.children[0]
	return SearchResult{
		BestMove: *best.move,
		Score:    int(best.valueSum / float64(best.visits) * 100),
		Nodes:    sims,
		Depth:    0,
	}
}

func moveKey(m *Move) string {
	return keyForSquare(m.From) + keyForSquare(m.To)
}

func evaluateMCTSValue(state *contracts.MatchState) float64 {
	if state.Status == "checkmate" {
		if state.Turn == "white" {
			return -1.0
		}
		return 1.0
	}
	if state.Status == "stalemate" || state.Status == "draw" {
		return 0.0
	}
	score := EvaluateWithModifiers(state.Board, state.Turn, state.LavaSquares, state.FortressZones, state.BombPieces, state.WhiteHand, state.BlackHand)
	clipped := math.Max(-20000.0, math.Min(20000.0, float64(score)))
	return clipped / 20000.0
}

func simpleHash(state *contracts.MatchState) uint64 {
	var h uint64
	if state.Turn == "white" {
		h |= 1
	}
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := state.Board[r][c]
			if p != nil {
				h ^= uint64(r*100+c*10+int(p.Type[0])) << 5
			}
		}
	}
	return h
}

type MCTSEngine struct {
	Config MCTSConfig
}

func NewMCTSEngine() *MCTSEngine {
	return &MCTSEngine{Config: DefaultMCTSConfig}
}

func (e *MCTSEngine) FindBestMove(state *contracts.MatchState) SearchResult {
	tt := NewTranspositionTable(1 << 16)
	return MCTSSearch(state, tt, e.Config)
}
