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

// Evaluator scores (p, ov, hands) from mover's own perspective -- the
// pluggable leaf-evaluation function a Searcher uses. defaultEvaluator
// (below) wraps eval.go's simple material+overlay placeholder;
// NNUEEvaluator (nnueeval.go) wraps a trained nnue.Network for Phase 3's
// SPRT comparison against it.
type Evaluator func(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color) int

func defaultEvaluator(p *core.Position, ov *core.CardOverlay, _ Hands, mover core.Color) int {
	return evaluateForMover(p, ov, mover)
}

// DefaultEvaluator exposes the placeholder material+overlay Evaluator to
// callers outside this package (e.g. cmd/nnue-selfplay) that need a
// working Evaluator without training or loading an nnue.Network.
var DefaultEvaluator Evaluator = defaultEvaluator

// Searcher holds the transposition table, node counter, and evaluator for
// one search (or one PIMC sample's search -- pimc.go creates a fresh
// Searcher per sample so samples don't share/pollute each other's TT).
type Searcher struct {
	tt    *TranspositionTable
	nodes int64
	eval  Evaluator
}

func NewSearcher() *Searcher {
	return &Searcher{tt: NewTranspositionTable(defaultTTSlots), eval: defaultEvaluator}
}

// NewSearcherWithEval builds a Searcher using a custom Evaluator (e.g.
// NNUEEvaluator) instead of the default placeholder -- how Phase 3's
// network gets compared against Phase 2's eval in the same search/gauntlet
// machinery.
func NewSearcherWithEval(eval Evaluator) *Searcher {
	return &Searcher{tt: NewTranspositionTable(defaultTTSlots), eval: eval}
}

// NewSearcherWithEvalAndTTSize is NewSearcherWithEval with an explicit TT
// slot count, for callers that construct a fresh Searcher per DECISION
// rather than per game/session (selfplay.go: a new Searcher for every
// ply, sometimes twice). defaultTTSlots (1<<20, ~33MB of ttEntry) is sized
// for one long-lived search, not for being allocated thousands of times
// in a tight loop -- doing that anyway is what caused a real out-of-memory
// runtime crash generating a multi-hundred-game, 200-ply self-play
// dataset (GC couldn't keep up with the allocation churn). A shallow,
// throwaway search needs nowhere near a million slots.
func NewSearcherWithEvalAndTTSize(eval Evaluator, ttSlots int) *Searcher {
	return &Searcher{tt: NewTranspositionTable(ttSlots), eval: eval}
}

func (s *Searcher) Nodes() int64 { return s.nodes }

// BestMove runs a fixed-depth negamax from (p, ov, hands) for mover, with
// BOTH hands fully known (no hidden-information handling -- that's
// pimc.go's job). Useful directly wherever both hands are legitimately
// known: tests, the card-tactics suite, self-play, and the gauntlet, none
// of which involve genuine hidden information.
//
// allowCard is normally true (a fresh turn starting from scratch); a
// caller driving a turn as two explicit steps -- self-play needs to know
// separately whether the root decision was a card or a move, to record
// and apply each step itself -- calls BestMove a second time with
// allowCard=false to get just the mandatory move that completes a turn
// after a card was already played.
//
// The third return value reports whether any action exists at all. A
// mover can legitimately have ZERO submittable actions here without the
// position being checkmate/stalemate: TerminalStatus is deliberately
// Frozen-blind (matching internal/match), but GenerateActions' move half
// is Frozen-aware, so "every mobile piece happens to be frozen right now"
// falls through Terminal Status's check and lands here instead. When ok
// is false, Action/score are meaningless placeholders -- callers must NOT
// apply the returned Move. This used to return a zero-valued
// actions.Action{} in that case, which callers trusted blindly: its zero
// Move is {From: a1, To: a1} (Square 0), a degenerate same-square "move"
// that core.Position.movePiece's `from.Bit()|to.Bit()` XOR trick corrupts
// silently -- collapsing to a single bit toggles the mover's own piece
// OFF the board instead of leaving it in place. Caught by self-play
// hitting exactly this frozen-lockout scenario deep into a real game (see
// selfplay_test.go's TestGenerateSelfPlayGameHandlesNoAvailableAction).
func (s *Searcher) BestMove(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, allowCard bool, depth int) (actions.Action, int, bool) {
	acts := actions.GenerateActions(p, ov, hands.For(mover), allowCard)
	if len(acts) == 0 {
		return actions.Action{}, s.eval(p, ov, hands, mover), false
	}
	orderActions(p, acts)

	best := acts[0]
	bestScore := -scoreInfinity
	alpha, beta := -scoreInfinity, scoreInfinity
	for _, a := range acts {
		score := s.applyAndRecurse(p, ov, hands, mover, a, allowCard, depth, 1, alpha, beta)
		if score > bestScore {
			bestScore = score
			best = a
		}
		if bestScore > alpha {
			alpha = bestScore
		}
	}
	return best, bestScore, true
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
		return s.eval(p, ov, hands, mover)
	}

	acts := actions.GenerateActions(p, ov, hands.For(mover), allowCard)
	if len(acts) == 0 {
		// TerminalStatus is Frozen-blind (matches internal/match's own
		// hasLegalMoveWithFusion), but GenerateActions' move half is
		// Frozen-AWARE (core.GenerateSubmittableMoves) -- e.g. the only
		// mobile piece is frozen. Not a terminal status, but nothing
		// submittable either; fall back to a static read rather than
		// treating an empty action list as a crash.
		return s.eval(p, ov, hands, mover)
	}
	orderActions(p, acts)

	best := -scoreInfinity
	for _, a := range acts {
		child := s.applyAndRecurse(p, ov, hands, mover, a, allowCard, depth, plyFromRoot, alpha, beta)
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
// actions identically. allowCard is the CURRENT mover's own remaining
// card-availability -- needed here (not just by the caller) because a
// shielded capture (see below) preserves it unchanged on a voided attempt.
func (s *Searcher) applyAndRecurse(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, a actions.Action, allowCard bool, depth, plyFromRoot, alpha, beta int) int {
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

	if target := p.PieceAt(a.Move.To); !target.IsNone() && target.Color != mover && ov.IsShielded(a.Move.To) {
		// Shield is an apply-time interceptor, not a legality rule (see
		// overlays.go's package comment) -- core.GenerateSubmittableMoves
		// happily generates a capture of a shielded piece, exactly like
		// internal/match's own movegen. internal/match voids the capture
		// entirely at application time instead (match_actions.go:73-84):
		// the board is untouched, the shield is spent, and -- the part
		// easy to miss -- the turn is NOT passed, so the attacker "wastes"
		// the attempt for free and keeps acting. Modeled here as: consume
		// the shield, then let mover choose AGAIN at the same depth/ply
		// with the same allowCard -- correct and loop-safe, since the
		// shield is gone by the second attempt, so retrying the identical
		// capture the next time around applies for real instead of
		// voiding again.
		ovSnapshot := ov.Clone()
		ov.TryConsumeShield(a.Move.To)
		score := s.negamax(p, ov, hands, mover, allowCard, depth, plyFromRoot, alpha, beta)
		*ov = *ovSnapshot
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
