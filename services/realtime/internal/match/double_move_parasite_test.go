package match

import (
	"strings"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	v1 "github.com/chess404/realtime/internal/engine/v1"
)

// craftParasiteDoubleMoveState builds a board where black's ONLY legal move
// is a capture whose PARASITE SIDE EFFECT, not the capture itself, gives
// check:
//
//   - white queen (5,3) checks black king (7,3) along the open 3rd file,
//     and white pawns (6,1)/(6,5) cover both flight squares the queen does
//     not attack, so capturing (5,3) is black's one legal move,
//   - black rook (0,0) aims at white king (0,7), blocked only by black
//     pawn (0,4), which carries a parasite link to (5,3).
//
// Capturing (5,3) destroys the linked black pawn (resolveParasiteEffects'
// second loop only checks that the REMOVED piece's own king stays safe),
// which uncovers rook (0,0)'s attack onto the white king. applyMove runs
// resolveParasiteEffects BEFORE the first-double-move guard, so the guard
// sees the discovered check and rejects -- while a plain applyMoveCopy
// (which models no card mechanics) sees a quiet capture and calls it safe.
func craftParasiteDoubleMoveState(service *Service, matchID string) *contracts.MatchState {
	state := service.getMatchContainer(matchID).state
	state.Board = emptyBoard()
	state.Board[0][7] = &contracts.Piece{Type: "king", Color: "white"}
	state.Board[5][3] = &contracts.Piece{Type: "queen", Color: "white"}
	state.Board[6][1] = &contracts.Piece{Type: "pawn", Color: "white"}
	state.Board[6][5] = &contracts.Piece{Type: "pawn", Color: "white"}
	state.Board[7][3] = &contracts.Piece{Type: "king", Color: "black"}
	state.Board[5][0] = &contracts.Piece{Type: "queen", Color: "black"}
	state.Board[0][0] = &contracts.Piece{Type: "rook", Color: "black"}
	state.Board[0][4] = &contracts.Piece{Type: "pawn", Color: "black", ParasiteTarget: "5,3"}
	state.Turn = "black"
	state.DoubleMove = &contracts.DoubleMoveState{Type: "diff", MovesLeft: 2}
	state.Moved = nil
	state.LastMove = nil
	state.MoveHistory = nil
	state.HalfMoveClock = 0
	return state
}

// TestFirstDoubleMoveRejectsParasiteDiscoveredCheck pins the server rule the
// engines' first-double-move filter must mirror: the guard evaluates the
// board AFTER resolveParasiteEffects, so a capture that destroys the
// mover's own linked blocker -- uncovering a discovered check the capture
// itself never made -- is rejected with the double-move guard's message.
func TestFirstDoubleMoveRejectsParasiteDiscoveredCheck(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{MatchID: "dm_parasite"}, now)
	craftParasiteDoubleMoveState(service, "dm_parasite")

	_, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "dm_parasite",
		PlayerID: "black_player",
		From:     &contracts.Square{Row: 5, Col: 0},
		To:       &contracts.Square{Row: 5, Col: 3},
	}, now.Add(time.Second))
	if err == nil || !strings.Contains(err.Error(), "first double move cannot put enemy king in check") {
		t.Fatalf("expected parasite-discovered check to be rejected by the double-move guard, got %v", err)
	}
}

// TestEngineMakeMoveSubmitsAcceptedFirstDoubleMove is the real-entry
// regression for the intermittent xgauntlet CI failure ("engine black
// submitted an invalid intent ... first double move cannot put enemy king
// in check"): the v1 engine's MakeMove, handed the exact state above where
// the only legal move IS the rejected capture, must refuse to submit it.
// Before FirstDoubleMoveRejected modeled parasite side effects, search
// returned the capture (the only candidate), the old filter's plain
// applyMoveCopy check called it safe, and applyMove rejected the intent;
// now the filter must leave MakeMove with nothing to submit.
func TestEngineMakeMoveSubmitsAcceptedFirstDoubleMove(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{MatchID: "dm_parasite_engine"}, now)
	state := craftParasiteDoubleMoveState(service, "dm_parasite_engine")

	co := v1.NewComputerOpponent(v1.DifficultyBeginner, "black")
	intent := co.MakeMove(state)
	if intent == nil {
		// The only legal candidate is server-rejected, so giving up is
		// the correct outcome (production and the xgauntlet harness
		// both route nil to their own progress-guarantee fallbacks).
		return
	}
	intent.PlayerID = "black_player"
	intent.PlayerSecret = "black-secret"
	intent.MatchID = "dm_parasite_engine"

	if _, err := applyTestIntent(service, *intent, now.Add(time.Second)); err != nil {
		t.Fatalf("MakeMove submitted %v->%v but applyMove rejected it: %v", intent.From, intent.To, err)
	}
}
