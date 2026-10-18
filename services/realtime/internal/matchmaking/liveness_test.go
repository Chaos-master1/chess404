package matchmaking

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// A ticket whose client stopped polling (closed tab, killed browser context)
// must not be handed to the next arrival as an opponent: that pairing opens a
// room the absent player never joins.
func TestQueueDoesNotPairAgainstAbandonedTicket(t *testing.T) {
	service := NewService()
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }

	ghost, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_ghost", 1200, "Ghost")
	if err != nil {
		t.Fatalf("enqueue ghost: %v", err)
	}

	service.now = func() time.Time { return base.Add(defaultQueuedLivenessTTL + time.Second) }
	arrival, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_live", 1200, "Live")
	if err != nil {
		t.Fatalf("enqueue arrival: %v", err)
	}
	if arrival.Status != StatusQueued {
		t.Fatalf("arrival must not pair against an unpolled ticket, got status=%s matchedWith=%s", arrival.Status, arrival.MatchedWith)
	}
	if got, _ := service.Get(ghost.TicketID); got.Status != StatusQueued {
		t.Fatalf("abandoned ticket must stay queued (not paired), got %s", got.Status)
	}
}

func TestQueuePollKeepsTicketMatchable(t *testing.T) {
	service := NewService()
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }

	waiter, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_waiter", 1200, "Waiter")
	if err != nil {
		t.Fatalf("enqueue waiter: %v", err)
	}
	for step := 1; step <= 4; step++ {
		at := base.Add(time.Duration(step) * (defaultQueuedLivenessTTL / 2))
		service.now = func() time.Time { return at }
		if got, ok := service.Poll(waiter.TicketID); !ok || got.Status != StatusQueued {
			t.Fatalf("poll %d: ticket=%#v ok=%v", step, got, ok)
		}
	}

	arrival, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_arrival", 1200, "Arrival")
	if err != nil {
		t.Fatalf("enqueue arrival: %v", err)
	}
	if arrival.Status != StatusMatched || arrival.MatchedWith != "guest_waiter" {
		t.Fatalf("a polling waiter must stay matchable, got status=%s matchedWith=%s", arrival.Status, arrival.MatchedWith)
	}
}

// A waiter that was briefly stale (throttled background tab) and then polls
// again must pair with whoever queued in the meantime -- pairing cannot only
// happen at enqueue time, or both players would wait forever.
func TestQueuePollPairsReturningWaiterWithQueuedArrival(t *testing.T) {
	service := NewService()
	creator := &captureMatchCreator{}
	service.SetMatchCreator(creator)
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }

	sleeper, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_sleeper", 1200, "Sleeper")
	if err != nil {
		t.Fatalf("enqueue sleeper: %v", err)
	}
	late := base.Add(defaultQueuedLivenessTTL + time.Second)
	service.now = func() time.Time { return late }
	arrival, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_arrival", 1200, "Arrival")
	if err != nil {
		t.Fatalf("enqueue arrival: %v", err)
	}
	if arrival.Status != StatusQueued {
		t.Fatalf("arrival should wait while the sleeper is stale, got %s", arrival.Status)
	}

	service.now = func() time.Time { return late.Add(5 * time.Second) }
	woke, ok := service.Poll(sleeper.TicketID)
	if !ok {
		t.Fatalf("poll sleeper: ticket missing")
	}
	if woke.Status != StatusMatched || woke.MatchedWith != "guest_arrival" {
		t.Fatalf("returning waiter must pair with the queued arrival, got status=%s matchedWith=%s", woke.Status, woke.MatchedWith)
	}
	if woke.CancelSecret != "" {
		t.Fatalf("poll response must never carry the cancel secret")
	}
	if got, _ := service.Get(arrival.TicketID); got.Status != StatusMatched || got.AssignedRoom != woke.AssignedRoom {
		t.Fatalf("arrival must be matched into the same room, got %#v", got)
	}
	if len(creator.assignments) != 1 {
		t.Fatalf("expected exactly one room created, got %d", len(creator.assignments))
	}
}

// Tickets persisted before LastSeenAt existed carry a zero value; they fall
// back to UpdatedAt rather than reading as instantly stale.
func TestQueueLegacyTicketWithoutLastSeenUsesUpdatedAt(t *testing.T) {
	service := NewService()
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }
	legacy, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_legacy", 1200, "Legacy")
	if err != nil {
		t.Fatalf("enqueue legacy: %v", err)
	}
	service.mu.Lock()
	stored := service.tickets[legacy.TicketID]
	stored.LastSeenAt = time.Time{}
	service.tickets[legacy.TicketID] = stored
	service.mu.Unlock()

	service.now = func() time.Time { return base.Add(time.Second) }
	arrival, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_new", 1200, "New")
	if err != nil {
		t.Fatalf("enqueue arrival: %v", err)
	}
	if arrival.Status != StatusMatched {
		t.Fatalf("fresh legacy ticket should still be matchable, got %s", arrival.Status)
	}
}
