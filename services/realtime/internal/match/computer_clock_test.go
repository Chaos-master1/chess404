package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestComputerThinkTimeIsNotBilledToTheHuman is a regression test for a live
// gameplay bug: every millisecond the engine spent thinking was deducted from
// its opponent's clock instead of its own.
//
// The computer path receives `now` frozen at the moment the HUMAN's intent was
// received (ApplyIntent -> autoPlayComputer -> computerCh -> computerWorker),
// but the engine then searches for up to several seconds before its move is
// applied. Applying it with that stale timestamp inverted the accounting
// twice over: syncClockForMutation saw `elapsed = now - StartedAt == 0` and
// returned early, so Black was never charged for the search; and applyMove
// then set Clock.StartedAt = now, backdated by the whole search, so the next
// sync computed `realNow - now` -- which includes the search -- and charged
// all of it to White.
//
// The test simulates a slow search by handing the computer path a deliberately
// backdated `now`, which is exactly the shape of the real bug.
func TestComputerThinkTimeIsNotBilledToTheHuman(t *testing.T) {
	service := NewService()
	start := time.Now().UTC()

	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "computer_clock",
		ModeID:            contracts.MatchModeComputer,
		Difficulty:        "beginner",
		ClockSeconds:      600,
		WhiteGuestID:      "guest_white",
		WhitePlayerSecret: "white-secret",
	}, start)

	c := service.getMatchContainer("computer_clock")
	if c == nil {
		t.Fatal("expected the match container to exist")
	}

	c.mu.Lock()
	// White has moved; it is now Black's (the computer's) turn, and Black's
	// clock has been running since `start`.
	c.state.Turn = "black"
	c.state.Clock.RunningFor = "black"
	startedAt := start.UnixMilli()
	c.state.Clock.StartedAt = &startedAt
	whiteBefore := c.state.Clock.WhiteMS
	blackBefore := c.state.Clock.BlackMS

	// Hand it the stale timestamp the real code path used to pass: `start`,
	// even though (as far as the clock is concerned) 4 seconds of engine
	// thinking have now elapsed.
	service.autoPlayComputerDepthLimited(c, start, 0)

	whiteAfter := c.state.Clock.WhiteMS
	blackAfter := c.state.Clock.BlackMS
	turnAfter := c.state.Turn
	c.mu.Unlock()

	if turnAfter != "white" {
		t.Fatalf("expected the computer to have moved and handed the turn back, got turn=%q", turnAfter)
	}

	// White never had the move during any of this, so White's clock must be
	// untouched. Under the bug White was the ONLY side charged.
	if whiteAfter != whiteBefore {
		t.Fatalf("the human's clock was charged for the engine's turn: white went %dms -> %dms (lost %dms)",
			whiteBefore, whiteAfter, whiteBefore-whiteAfter)
	}

	// And Black must actually be charged for the time it spent, rather than
	// moving for free.
	if blackAfter >= blackBefore {
		t.Fatalf("expected the engine to be charged for its own thinking time, but black went %dms -> %dms",
			blackBefore, blackAfter)
	}
}
