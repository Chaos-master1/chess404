package search

import (
	"sort"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

const (
	scoreMate      = 1_000_000
	scoreInfinity  = 2_000_000
	defaultTTSlots = 1 << 20
)

// Hands bundles both sides' hands for one search. For the searching
// engine's own color this is its REAL hand; for the opponent, in a
// fair-play search (pimc.go), it is one sampled hand for the CURRENT PIMC
// iteration, held fixed for that iteration's entire search -- never the
// opponent's actual hand.
type Hands struct {
	White, Black actions.Hand
}

func (h Hands) For(c core.Color) actions.Hand {
	if c == core.White {
		return h.White
	}
	return h.Black
}

func (h Hands) With(c core.Color, hand actions.Hand) Hands {
	if c == core.White {
		h.White = hand
	} else {
		h.Black = hand
	}
	return h
}

// Searcher holds the transposition table and node counter for one search
// (or one PIMC sample's search -- pimc.go creates a fresh Searcher per
// sample so samples don't share/pollute each other's TT).
type Searcher struct {
	tt    *TranspositionTable
	nodes int64
}

func NewSearcher() *Searcher {
	return &Searcher{tt: NewTranspositionTable(defaultTTSlots)}
}

func (s *Searcher) Nodes() int64 { return s.nodes }

// BestMove runs a fixed-depth negamax from (p, ov, hands) for mover, with
// BOTH hands fully known (no hidden-information handling -- that's
// pimc.go's job). Useful directly wherever both hands are legitimately
// known: tests, the card-tactics suite, self-play, and the gauntlet, none
// of which involve genuine hidden information.
func (s *Searcher) BestMove(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, depth int) (actions.Action, int) {
	acts := actions.GenerateActions(p, ov, hands.For(mover), true)
	if len(acts) == 0 {
		return actions.Action{}, evaluateForMover(p, ov, mover)
	}
	orderActions(p, acts)

	best := acts[0]
	bestScore := -scoreInfinity
	alpha, beta := -scoreInfinity, scoreInfinity
	for _, a := range acts {
		score := s.applyAndRecurse(p, ov, hands, mover, a, depth, 1, alpha, beta)
		if score > bestScore {
			bestScore = score
			best = a
		}
		if bestScore > alpha {
			alpha = bestScore
		}
	}
	return best, bestScore
}

// negamax is the recursive alpha-beta core. depth counts remaining move
// budget (playing a card does NOT consume depth -- the same mover
// immediately gets another decision, matching "cards as first-class
// search-tree nodes" without making a card-heavy turn artificially
// cheaper or costlier to search than an equivalent card-free one).
// plyFromRoot is tracked separately from depth so mate scoring prefers
// the fastest mate regardless of any future depth extensions/reductions.
func (s *Searcher) negamax(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, allowCard bool, depth, plyFromRoot, alpha, beta int) int {
	s.nodes++
	alphaOrig := alpha

	key := p.Hash() ^ ov.Hash()
	if score, ok := s.tt.Probe(key, depth, alpha, beta); ok {
		return score
	}

	switch actions.TerminalStatus(p, ov, hands.For(mover)) {
	case core.Checkmate:
		return -(scoreMate - plyFromRoot)
	case core.Stalemate:
		return 0
	}

	if depth <= 0 {
		return evaluateForMover(p, ov, mover)
	}

	acts := actions.GenerateActions(p, ov, hands.For(mover), allowCard)
	if len(acts) == 0 {
		// TerminalStatus is Frozen-blind (matches internal/match's own
		// hasLegalMoveWithFusion), but GenerateActions' move half is
		// Frozen-AWARE (core.GenerateSubmittableMoves) -- e.g. the only
		// mobile piece is frozen. Not a terminal status, but nothing
		// submittable either; fall back to a static read rather than
		// treating an empty action list as a crash.
		return evaluateForMover(p, ov, mover)
	}
	orderActions(p, acts)

	best := -scoreInfinity
	for _, a := range acts {
		child := s.applyAndRecurse(p, ov, hands, mover, a, depth, plyFromRoot, alpha, beta)
		if child > best {
			best = child
		}
		if best > alpha {
			alpha = best
		}
		if alpha >= beta {
			break
		}
	}

	s.tt.Store(key, depth, best, alphaOrig, beta)
	return best
}

// applyAndRecurse applies one action (card or move) and returns the
// resulting score from mover's OWN perspective -- the same convention
// negamax's return value uses, so callers (BestMove's root loop,
// negamax's own inner loop) use the result DIRECTLY with no extra
// negation. This is deliberately NOT "always negate the child call" the
// way a plain negamax recursion is, because a card action does not pass
// the turn: playing a card keeps the SAME mover to act next (see
// action.go's turn model), so there is no perspective flip to undo. A
// move does pass the turn, so that branch negates and swaps the window
// exactly like a standard negamax child call. Getting this asymmetry
// backwards (blanket-negating both cases, or neither) was a real bug
// caught by TestBestMoveCoordinatesFreezeThenCapture: it silently
// negates every card action's score, so the search prefers freezing
// whichever piece it should least prefer to freeze.
//
// Shared by BestMove's root loop and negamax's inner loop so both apply
// actions identically.
func (s *Searcher) applyAndRecurse(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, a actions.Action, depth, plyFromRoot, alpha, beta int) int {
	if a.Kind == actions.ActionCard {
		u := actions.ApplyCardAction(p, ov, a)
		newHand := hands.For(mover).Without(a.Card.ID)
		// Same mover continues (a card play never passes the turn) -- no
		// perspective flip, so the window and the returned score both
		// pass through unchanged.
		score := s.negamax(p, ov, hands.With(mover, newHand), mover, false, depth, plyFromRoot, alpha, beta)
		actions.UndoCardAction(p, ov, u)
		return score
	}
	undo := applyMoveWithTicks(p, ov, mover, a.Move)
	// The turn passes -- standard negamax child call: negate and swap the
	// window, negate the result to convert from the opponent's returned
	// perspective back to mover's.
	score := -s.negamax(p, ov, hands, mover.Opposite(), true, depth-1, plyFromRoot+1, -beta, -alpha)
	undo()
	return score
}

// orderActions is a simple capture-first ordering (MVV-LVA-ish: bigger
// captured piece first, cheaper attacker preferred among equal captures),
// with card actions ranked between captures and quiet moves. No killer
// moves, history heuristic, or counter-move table -- see the package
// comment for what's deferred.
func orderActions(p *core.Position, acts []actions.Action) {
	sort.SliceStable(acts, func(i, j int) bool {
		return actionOrderScore(p, acts[i]) > actionOrderScore(p, acts[j])
	})
}

func actionOrderScore(p *core.Position, a actions.Action) int {
	if a.Kind == actions.ActionMove {
		captured := p.PieceAt(a.Move.To)
		if captured.IsNone() {
			return 0
		}
		attacker := p.PieceAt(a.Move.From)
		return 1000 + captured.Type.Value()*10 - attacker.Type.Value()
	}
	return 500
}
