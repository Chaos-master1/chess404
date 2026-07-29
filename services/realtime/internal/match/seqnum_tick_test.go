package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestPeriodicBroadcastTickDoesNotAdvanceSeqNum is a regression test for a
// live production bug: startBroadcaster's once-a-second tick called
// processMatchBroadcast, whose final branch unconditionally re-broadcast the
// current snapshot (purely so connected clients see the clock keep counting
// down) via the same broadcastLocked that ApplyIntent's real mutations use --
// which mints a new seq every time. ApplyIntent rejects a move whenever the
// client's expectedSeqNum is behind the server's current seq. Since nothing
// about board/turn/status actually changes on a cosmetic clock tick, bumping
// seq for it meant a client's move could be rejected as "stale" purely
// because a per-second heartbeat broadcast raced its click by a few tens of
// milliseconds -- observed live as a steady trickle of 409s roughly every
// 10-30 seconds of active play, each one self-recovering (via a separate,
// already-fixed client resync) but still visibly failing every time.
func TestPeriodicBroadcastTickDoesNotAdvanceSeqNum(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "seq_tick_test",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	if err := service.HeartbeatPresence("seq_tick_test", testPresence("guest-white"), now); err != nil {
		t.Fatalf("expected white heartbeat to succeed, got %v", err)
	}
	if err := service.HeartbeatPresence("seq_tick_test", testPresence("guest-black"), now); err != nil {
		t.Fatalf("expected black heartbeat to succeed, got %v", err)
	}

	// A live subscriber is required for processMatchBroadcast's cosmetic
	// branch to run at all (it returns early when len(c.subs) == 0).
	_, unsubscribe, _, err := service.Subscribe("seq_tick_test", "guest-white", "white-secret")
	if err != nil {
		t.Fatalf("expected subscribe to succeed, got %v", err)
	}
	defer unsubscribe()

	c := service.getMatchContainer("seq_tick_test")
	c.mu.Lock()
	seqBefore := c.seqNum
	c.mu.Unlock()

	// Five ticks, no moves, no card plays, no presence changes in between --
	// purely the clock continuing to run.
	for i := 1; i <= 5; i++ {
		service.collectAndBroadcast(now.Add(time.Duration(i) * time.Second))
	}

	c.mu.Lock()
	seqAfter := c.seqNum
	c.mu.Unlock()
	if seqAfter != seqBefore {
		t.Fatalf("expected 5 cosmetic clock ticks with no game-state change to leave seqNum unchanged, went from %d to %d", seqBefore, seqAfter)
	}

	// A move built against the pre-tick seq must still be accepted -- this
	// is what actually broke live: a client's expectedSeqNum, current as of
	// its last real update, got treated as stale purely by tick count.
	resp, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:           "make_move",
		MatchID:        "seq_tick_test",
		PlayerID:       "guest-white",
		ExpectedSeqNum: seqBefore,
		From:           &contracts.Square{Row: 1, Col: 4},
		To:             &contracts.Square{Row: 3, Col: 4},
	}, now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("expected move built against the pre-tick seq to succeed, got %v", err)
	}
	if resp.Match.Status != "active" {
		t.Fatalf("expected match to remain active after a legal opening move, got %q", resp.Match.Status)
	}
}
