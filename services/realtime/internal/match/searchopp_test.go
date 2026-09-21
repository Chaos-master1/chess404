package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	v1 "github.com/chess404/realtime/internal/engine/v1"
)

// Regression tests for the search-backed composite opponent. The invariants
// that matter: every intent it produces must be ACCEPTED by this package's
// own rules (applyIntent is the trust anchor), the fallback chain must honor
// the active double-move constraint, and the new engine must actually prefer
// winning material when it is free.

func newSearchTestMatch(t *testing.T, service *Service, matchID string) *matchContainer {
	t.Helper()
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           matchID,
		ModeID:            contracts.MatchModeComputer,
		Difficulty:        "medium",
		WhiteGuestID:      "guest_white",
		WhitePlayerSecret: "white-secret",
	}, time.Now())
	c, ok := service.matches.Load(matchID)
	if !ok {
		t.Fatalf("match %s not found", matchID)
	}
	return c
}

// Every intent the composite produces across a series of full (short-budget)
// games must be accepted by applyIntent. A rejection here means the search
// produced something the real rules consider illegal -- the exact failure
// class this opponent exists to avoid.
func TestSearchOpponentIntentsAlwaysLegal(t *testing.T) {
	if testing.Short() {
		t.Skip("full-game conformance is a long test")
	}
	for game := 0; game < 2; game++ {
		service := NewService()
		matchID := "sopp_legal"
		c := newSearchTestMatch(t, service, matchID)

		c.mu.Lock()
		opp := newSearchOpponent(v1.DifficultyMedium)
		moves := 0
		// Drive a FULL game: black (the search opponent) answers every black
		// turn; white is driven by the package's own guaranteed-legal fallback
		// so the game keeps moving and the search is exercised across many
		// distinct positions, not just the opening.
		for moves < 80 {
			if c.state.Status != "active" {
				break
			}
			if c.state.Turn == "white" {
				from, to, ok := firstLegalMoveForColorConstrained(c.state)
				if !ok {
					break // genuine mate/stalemate
				}
				intent := contracts.PlayerIntent{
					Type:         "make_move",
					MatchID:      c.state.MatchID,
					PlayerID:     c.state.WhiteGuestID,
					PlayerSecret: c.state.WhitePlayerSecret,
					From:         &from,
					To:           &to,
				}
				events, err := applyIntent(c.state, intent, time.Now())
				if err != nil {
					t.Fatalf("game %d: white fallback move rejected: %v", game, err)
				}
				c.events = append(c.events, events...)
				continue
			}
			intent := opp.MakeMove(c.state)
			if intent == nil {
				t.Fatalf("game %d: opponent returned nil on move %d", game, moves)
			}
			intent.PlayerID = c.state.BlackGuestID
			intent.PlayerSecret = c.state.BlackPlayerSecret
			events, err := applyIntent(c.state, *intent, time.Now())
			if err != nil {
				t.Fatalf("game %d: opponent intent rejected on move %d: %v (intent %+v)", game, moves, err, intent)
			}
			c.events = append(c.events, events...)
			moves++
		}
		c.mu.Unlock()
		if moves == 0 {
			t.Fatalf("game %d: opponent never moved", game)
		}
		if moves < 10 {
			t.Fatalf("game %d: game ended suspiciously early (%d black moves)", game, moves)
		}
		t.Logf("game %d: %d black moves, final status=%s turn=%s", game, moves, c.state.Status, c.state.Turn)
	}
}

// The last-resort fallback (firstLegalMoveForColorConstrained) must produce a
// move applyMove accepts even while a twin double move constrains the reply.
// This is the regression for "fallback legal move rejected: twin double move
// requires moving a different piece".
func TestConstrainedFallbackRespectsDoubleMove(t *testing.T) {
	service := NewService()
	c := newSearchTestMatch(t, service, "sopp_fallback")

	c.mu.Lock()
	defer c.mu.Unlock()

	// Simulate the second half of a twin (diff) double move: black played a
	// card granting it and moved the tracked piece first; now a DIFFERENT
	// piece must move.
	tracked := contracts.Square{Row: 6, Col: 0}
	c.state.DoubleMove = &contracts.DoubleMoveState{
		Type:      "diff",
		MovesLeft: 1,
		TrackedSq: &tracked,
	}
	c.state.Turn = "black"
	c.state.Status = "active"

	from, to, ok := firstLegalMoveForColorConstrained(c.state)
	if !ok {
		t.Fatal("expected a constrained legal move to exist")
	}
	if from.Row == tracked.Row && from.Col == tracked.Col {
		t.Fatal("fallback returned the tracked piece for a diff double move")
	}

	intent := contracts.PlayerIntent{
		Type:         "make_move",
		MatchID:      c.state.MatchID,
		PlayerID:     c.state.BlackGuestID,
		PlayerSecret: c.state.BlackPlayerSecret,
		From:         &from,
		To:           &to,
	}
	if _, err := applyIntent(c.state, intent, time.Now()); err != nil {
		t.Fatalf("constrained fallback move rejected by the rules: %v", err)
	}
}

// Sanity check that the new engine is actually playing chess: from a
// hand-built position with a free queen capture, the search must take it
// rather than playing a quiet move.
func TestSearchOpponentTakesHangingQueen(t *testing.T) {
	service := NewService()
	c := newSearchTestMatch(t, service, "sopp_tactics")

	c.mu.Lock()
	defer c.mu.Unlock()

	// Sparse board: white king e1, white queen d5 (undefended), black king
	// e8, black rook d8 adjacent to the queen.
	state := c.state
	state.Board = makeBoard()
	// Wipe the initial position so the rook actually reaches the queen --
	// the default board's own pawns would block it.
	for r := range state.Board {
		for col := range state.Board[r] {
			state.Board[r][col] = nil
		}
	}
	state.Board[0][4] = &contracts.Piece{Type: "king", Color: "white"}
	state.Board[3][3] = &contracts.Piece{Type: "queen", Color: "white"}
	state.Board[7][4] = &contracts.Piece{Type: "king", Color: "black"}
	state.Board[7][3] = &contracts.Piece{Type: "rook", Color: "black"}
	state.Turn = "black"
	state.LastMove = nil
	state.Moved = nil
	state.HalfMoveClock = 0
	state.FullMoveNum = 20

	opp := newSearchOpponent(v1.DifficultyExpert)
	// Call the chess search directly: the composite would be entitled to play
	// a card here (the test match deals the full catalog), which would make
	// this assertion flap on the card brain rather than test the search.
	intent, err := opp.searchMoveIntent(state)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if intent == nil || intent.From == nil || intent.To == nil {
		t.Fatalf("expected a move intent, got %+v", intent)
	}
	if intent.From.Col != 3 || intent.From.Row != 7 {
		t.Fatalf("expected the rook on d8 to move, got from %+v", intent.From)
	}
	if intent.To.Row != 3 || intent.To.Col != 3 {
		t.Fatalf("expected the rook to take the queen on d5, got to %+v", intent.To)
	}
}

// searchBudgetFor must keep difficulty ordering monotonic: higher tiers
// never think less than lower ones.
func TestSearchBudgetsAreMonotonic(t *testing.T) {
	order := []v1.Difficulty{
		v1.DifficultyBeginner, v1.DifficultyEasy,
		v1.DifficultyMedium, v1.DifficultyHard, v1.DifficultyExpert,
	}
	scores := make([]int, 0, len(order))
	for _, d := range order {
		opp := newSearchOpponent(d)
		limit, samples := searchBudgetFor(opp.inner)
		scores = append(scores, int(limit)*samples)
		_ = opp // construction must succeed for every tier
	}
	for i := 1; i < len(scores); i++ {
		if scores[i] < scores[i-1] {
			t.Fatalf("difficulty budget regressed at tier %d: %d < %d", i, scores[i], scores[i-1])
		}
	}
}
