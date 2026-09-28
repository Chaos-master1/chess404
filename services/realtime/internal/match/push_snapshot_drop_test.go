package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// newDroppingService builds a match with a subscriber whose 128-slot buffer is
// pre-filled so the next broadcast overflows it.
func newDroppingService(t *testing.T) (*Service, *matchContainer, chan contracts.MatchSnapshotResponse) {
	t.Helper()
	s := NewService()
	t.Cleanup(s.Close)
	created := s.CreateMatch(contracts.CreateMatchRequest{
		WhiteGuestID: "guest_w",
		BlackGuestID: "guest_b",
	}, time.Now().UTC())
	matchID := created.Match.MatchID

	c, ok := s.matches.Load(matchID)
	if !ok {
		t.Fatalf("match container missing")
	}
	// Fill the subscriber channel to capacity (128) so the next push drops.
	ch := make(chan contracts.MatchSnapshotResponse, 128)
	c.mu.Lock()
	c.subs[ch] = "white"
	for i := 0; i < 128; i++ {
		ch <- contracts.MatchSnapshotResponse{SeqNum: int64(i)}
	}
	c.mu.Unlock()
	return s, c, ch
}

// assertClosed drains any buffered items and verifies the channel is closed.
func assertClosed(t *testing.T, ch chan contracts.MatchSnapshotResponse, label string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		default:
			t.Fatalf("%s: channel not closed", label)
		}
	}
	t.Fatalf("%s: channel never closed after draining buffer", label)
}

// After a buffer-overflow drop the channel must be removed from c.subs and
// closed exactly once. Leaving it in the map meant every later broadcast
// panicked into recover for that client forever, the slot counted against the
// per-match subscriber cap, and the WS handler's eventual unsubscribe()
// (which closes only when the entry is present) closed the already-closed
// channel via a different path -- an unrecovered panic on a hijacked
// goroutine that could take down the process.
func TestPushSnapshotDropRemovesSubscriber(t *testing.T) {
	s, c, ch := newDroppingService(t)

	s.broadcastLocked(c, contracts.MatchSnapshotResponse{Match: *c.state})

	c.mu.Lock()
	_, stillSubscribed := c.subs[ch]
	c.mu.Unlock()
	if stillSubscribed {
		t.Fatalf("dropped subscriber channel was not removed from c.subs")
	}
	assertClosed(t, ch, "dropped subscriber")

	// The unsubscribe closure Subscribe() installs must now be a no-op for
	// this channel instead of panicking on close-of-closed.
	unsubscribe := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, present := c.subs[ch]; present {
			delete(c.subs, ch)
			close(ch)
		}
	}
	unsubscribe()
}

// The leak variant: if drops never removed the subscriber, 50 dropped clients
// would exhaust maxSubscribersPerMatch and lock the match to new viewers.
func TestPushSnapshotDropFreesSubscriberSlot(t *testing.T) {
	s, c, _ := newDroppingService(t)

	for i := 0; i < 3; i++ {
		c.mu.Lock()
		ch2 := make(chan contracts.MatchSnapshotResponse, 1)
		c.subs[ch2] = "white"
		// Pre-fill so the broadcast actually drops instead of buffering.
		ch2 <- contracts.MatchSnapshotResponse{SeqNum: int64(i)}
		c.mu.Unlock()
		s.broadcastLocked(c, contracts.MatchSnapshotResponse{Match: *c.state})
	}

	c.mu.Lock()
	count := len(c.subs)
	c.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 subscribers after drops, got %d", count)
	}
}

// Healthy subscribers must keep receiving after a slow one is dropped.
func TestBroadcastContinuesAfterDrop(t *testing.T) {
	s, c, slow := newDroppingService(t)

	healthy := make(chan contracts.MatchSnapshotResponse, 8)
	c.mu.Lock()
	c.subs[healthy] = "black"
	c.mu.Unlock()

	s.broadcastLocked(c, contracts.MatchSnapshotResponse{Match: *c.state})

	select {
	case snap := <-healthy:
		if snap.Match.MatchID == "" {
			t.Fatalf("healthy subscriber got empty match")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("healthy subscriber received nothing after slow-subscriber drop")
	}

	assertClosed(t, slow, "slow subscriber")
}
