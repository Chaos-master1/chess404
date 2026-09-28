package matchmaking

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

type failingMatchCreator struct {
	mu       sync.Mutex
	failures int
	calls    int
}

// CreateMatch is called without the queue's mutex held (two-phase pairing),
// so concurrent pairings can invoke it simultaneously.
func (c *failingMatchCreator) CreateMatch(assignment MatchAssignment) error {
	c.mu.Lock()
	c.calls++
	calls := c.calls
	failures := c.failures
	c.mu.Unlock()
	if calls <= failures {
		return errors.New("match service unavailable")
	}
	return nil
}

// One transient create failure must kick NOBODY out of the queue: the pairing
// rollback re-queues both tickets instead of deleting them, so the opponent
// (who may have waited minutes) keeps their queue position and can pair again.
func TestRollbackRequeuesTicketsOnCreateFailure(t *testing.T) {
	service := NewService()
	creator := &failingMatchCreator{failures: 1}
	service.SetMatchCreator(creator)

	first, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha")
	if err != nil {
		t.Fatalf("enqueue first ticket: %v", err)
	}
	if first.Status != StatusQueued {
		t.Fatalf("expected first ticket queued, got %s", first.Status)
	}

	// Second enqueue pairs with the waiting ticket; CreateMatch fails and the
	// caller gets an error plus a zero ticket.
	second, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo")
	if err == nil {
		t.Fatalf("expected enqueue to fail when create fails")
	}
	if second.TicketID != "" {
		t.Fatalf("expected zero ticket on failed pairing, got %#v", second)
	}

	// The opponent's ticket must still exist and be re-queued (not deleted).
	recovered, ok := service.Get(first.TicketID)
	if !ok {
		t.Fatalf("opponent ticket was deleted on rollback; it must be re-queued instead")
	}
	if recovered.Status != StatusQueued {
		t.Fatalf("expected opponent ticket re-queued, got %s", recovered.Status)
	}
	if recovered.AssignedRoom != "" || recovered.SeatColor != "" || recovered.MatchedAt != nil {
		t.Fatalf("expected opponent ticket matched fields cleared, got %#v", recovered)
	}
	if got, ok := service.Get(secondOf(t, service, "guest_b").TicketID); ok && got.Status == StatusMatched {
		t.Fatalf("failed pairing left a matched ticket behind: %#v", got)
	}

	// A fresh joiner pairs with the re-queued opponent once the creator recovers.
	pair, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_c", 1215, "Charlie")
	if err != nil {
		t.Fatalf("expected successful pairing after creator recovery, got %v", err)
	}
	if pair.Status != StatusMatched || pair.AssignedRoom == "" {
		t.Fatalf("expected matched ticket after recovery, got %#v", pair)
	}
}

// secondOf is a tiny helper so the failure-path test can assert guest_b's
// stored ticket shape without relying on the zero return value.
func secondOf(t *testing.T, service *Service, guestID string) Ticket {
	t.Helper()
	ticket, ok := service.FindActiveTicket(guestID, "")
	if !ok {
		return Ticket{}
	}
	return ticket
}

// A transient create failure must leave no ghost reserved ticket behind: both
// participants end up queued again, and a same-lane rejoin during the outage
// is idempotent (returns the existing queued ticket) instead of stacking a
// second one.
func TestFailedPairingLeavesNoReservedGhostTickets(t *testing.T) {
	service := NewService()
	creator := &failingMatchCreator{failures: 1}
	service.SetMatchCreator(creator)

	if _, err := service.EnqueueWithAccount(QueueRated, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha", "acct_a", 0, 0); err != nil {
		t.Fatalf("enqueue first ticket: %v", err)
	}
	if _, err := service.EnqueueWithAccount(QueueRated, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo", "acct_b", 0, 0); err == nil {
		t.Fatalf("expected first pairing attempt to fail")
	}

	// Same guest rejoins the same lane during the outage: idempotent, returns
	// their existing queued ticket without another create attempt.
	rejoin, err := service.EnqueueWithAccount(QueueRated, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo", "acct_b", 0, 0)
	if err != nil {
		t.Fatalf("expected same-lane rejoin during outage to be idempotent, got %v", err)
	}
	if rejoin.Status != StatusQueued {
		t.Fatalf("expected rejoin to surface the queued ticket, got %s", rejoin.Status)
	}
	if creator.calls != 1 {
		t.Fatalf("idempotent rejoin must not call the creator again, calls=%d", creator.calls)
	}

	// Creator recovers: next joiner pairs with one of the queued tickets.
	recovered := &failingMatchCreator{failures: 0}
	service.SetMatchCreator(recovered)
	pair, err := service.EnqueueWithAccount(QueueRated, contracts.MatchModeOpenCards, "guest_c", 1215, "Charlie", "acct_c", 0, 0)
	if err != nil {
		t.Fatalf("expected pairing after recovery, got %v", err)
	}
	if pair.Status != StatusMatched {
		t.Fatalf("expected matched ticket after recovery, got %s", pair.Status)
	}
	// The pairing consumed one waiter: 2 matched (the pair) + 1 still queued
	// (guest_b), and nothing else -- no ghosts, no deleted tickets.
	listed := service.List(QueueRated, contracts.MatchModeOpenCards)
	if len(listed) != 3 {
		t.Fatalf("expected exactly 3 tickets after recovery pairing, got %d", len(listed))
	}
	matchedCount, queuedCount := 0, 0
	for _, ticket := range listed {
		switch ticket.Status {
		case StatusMatched:
			matchedCount++
		case StatusQueued:
			queuedCount++
		}
	}
	if matchedCount != 2 || queuedCount != 1 {
		t.Fatalf("expected 2 matched + 1 queued after recovery, got %d matched + %d queued", matchedCount, queuedCount)
	}
}

// Concurrent enqueues against a pool of waiting tickets: exactly one joiner
// may pair with each waiting ticket. The old unlock-around-CreateMatch window
// let two joiners pair with the same opponent (TOCTOU double-pairing).
func TestConcurrentEnqueueNeverDoublePairs(t *testing.T) {
	service := NewService()
	// Failing creator while seeding the pool: without it the waiters would
	// pair with EACH OTHER during the fill loop and leave nothing to join.
	service.SetMatchCreator(&failingMatchCreator{failures: 1024})

	const waiting = 24
	for i := 0; i < waiting; i++ {
		// Fill tolerates errors: an enqueue that finds an earlier waiter
		// attempts a pairing, which the failing creator rejects, and the
		// rollback re-queues both tickets. Either way the guest ends queued.
		_, _ = service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, waitingGuestID(i), 1200, "Waiter")
	}
	queuedAfterFill := 0
	for _, ticket := range service.List(QueueCasual, contracts.MatchModeOpenCards) {
		if ticket.Status == StatusQueued {
			queuedAfterFill++
		}
	}
	if queuedAfterFill != waiting {
		t.Fatalf("expected all %d waiters queued after fill, got %d", waiting, queuedAfterFill)
	}

	creator := &captureMatchCreator{}
	service.SetMatchCreator(creator)

	const joiners = waiting
	var wg sync.WaitGroup
	errCh := make(chan error, joiners)
	for i := 0; i < joiners; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ticket, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, joinerGuestID(n), 1205, "Joiner")
			if err != nil {
				errCh <- err
				return
			}
			if ticket.Status != StatusMatched {
				errCh <- errors.New("joiner expected to pair immediately with a waiting ticket")
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent enqueue failed: %v", err)
	}

	// Every waiting ticket must be matched exactly once, with a distinct
	// opponent and room.
	rooms := make(map[string]int)
	opponents := make(map[string]int)
	for _, waiter := range service.List(QueueCasual, contracts.MatchModeOpenCards) {
		if waiter.Status != StatusMatched {
			t.Fatalf("waiting ticket %s not matched after concurrent joins: %#v", waiter.TicketID, waiter)
		}
		rooms[waiter.AssignedRoom]++
		opponents[waiter.MatchedWith]++
		if waiter.MatchedWith == "" {
			t.Fatalf("matched ticket missing opponent: %#v", waiter)
		}
	}
	if len(creator.assignments) != waiting {
		t.Fatalf("expected %d match creations, got %d", waiting, len(creator.assignments))
	}
	for room, count := range rooms {
		if count != 2 {
			t.Fatalf("room %s claims %d tickets; a pairing was reused", room, count)
		}
	}
	for opponent, count := range opponents {
		if count != 1 {
			t.Fatalf("guest %s matched %d times: double-pairing TOCTOU regression", opponent, count)
		}
	}
}

// A guest whose ticket is still marked matched may re-join the SAME lane and
// must not be wedged behind a 409 for the full matched-TTL window. The release
// must NOT touch other lanes (different queue/mode still 409s).
func TestRejoinSameLaneReleasesStaleMatchedTicket(t *testing.T) {
	service := NewService()
	if _, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha"); err != nil {
		t.Fatalf("enqueue waiting ticket: %v", err)
	}
	first, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo")
	if err != nil {
		t.Fatalf("enqueue pairing ticket: %v", err)
	}
	if first.Status != StatusMatched {
		t.Fatalf("expected matched ticket, got %s", first.Status)
	}

	// Re-join the same lane: stale matched ticket is released, fresh ticket issued.
	fresh, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo")
	if err != nil {
		t.Fatalf("expected same-lane rejoin to release the stale matched ticket, got %v", err)
	}
	if fresh.TicketID == first.TicketID {
		t.Fatalf("expected a fresh ticket after stale release, got the same one")
	}

	// A DIFFERENT lane must still refuse while any active ticket exists.
	if _, err := service.EnqueueWithAccount(QueueRated, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo", "acct_b", 0, 0); err == nil {
		t.Fatalf("expected different-lane join to fail with an active ticket")
	}
	// The released stale ticket is cancelled, not active.
	stale, ok := service.Get(first.TicketID)
	if !ok {
		t.Fatal("expected stale ticket record to remain (cancelled) after release")
	}
	if stale.Status != StatusCancelled {
		t.Fatalf("expected stale matched ticket cancelled after release, got %s", stale.Status)
	}
}

// DELETE authorization: only the holder of the per-ticket cancel secret may
// cancel. Wrong or missing secret is refused; the internal-service caller
// bypasses the secret via CancelByService; List output never leaks the secret.
func TestCancelRequiresTicketSecret(t *testing.T) {
	service := NewService()
	ticket, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha")
	if err != nil {
		t.Fatalf("enqueue ticket: %v", err)
	}
	if ticket.CancelSecret == "" {
		t.Fatal("expected enqueue to issue a cancel secret")
	}

	// Wrong secret refused, ticket stays queued.
	if _, ok, err := service.Cancel(ticket.TicketID, "cxl_wrong"); err == nil || ok {
		t.Fatalf("expected wrong secret to be refused, got ok=%v err=%v", ok, err)
	}
	if got, ok := service.Get(ticket.TicketID); !ok || got.Status != StatusQueued {
		t.Fatalf("ticket must remain queued after refused cancel, got %#v ok=%v", got, ok)
	}
	// Empty secret refused.
	if _, ok, err := service.Cancel(ticket.TicketID, ""); err == nil || ok {
		t.Fatalf("expected empty secret to be refused, got ok=%v err=%v", ok, err)
	}

	// Internal-service path bypasses the per-ticket secret.
	if _, ok, err := service.CancelByService(ticket.TicketID); !ok || err != nil {
		t.Fatalf("expected internal service cancel to succeed, got ok=%v err=%v", ok, err)
	}
	if got, ok := service.Get(ticket.TicketID); !ok || got.Status != StatusCancelled {
		t.Fatalf("expected cancelled ticket after service cancel, got %#v ok=%v", got, ok)
	}
}

// List output must never carry the cancel credential.
func TestListOmitsCancelSecret(t *testing.T) {
	service := NewService()
	if _, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha"); err != nil {
		t.Fatalf("enqueue ticket: %v", err)
	}
	for _, item := range service.List(QueueCasual, contracts.MatchModeOpenCards) {
		if item.CancelSecret != "" {
			t.Fatalf("list leaked a cancel secret for ticket %s", item.TicketID)
		}
	}
}

func waitingGuestID(n int) string {
	return "waiter_" + time.Now().UTC().Format("150405") + "_" + itoa(n)
}

func joinerGuestID(n int) string {
	return "joiner_" + time.Now().UTC().Format("150405") + "_" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// slowMatchCreator sleeps before (successfully) creating, simulating the
// ~2s WAN round trip to match-service in production.
type slowMatchCreator struct {
	delay    time.Duration
	mu       sync.Mutex
	assignments []MatchAssignment
}

func (c *slowMatchCreator) CreateMatch(assignment MatchAssignment) error {
	time.Sleep(c.delay)
	c.mu.Lock()
	c.assignments = append(c.assignments, assignment)
	c.mu.Unlock()
	return nil
}

// The point of two-phase pairing: while one pairing's creator call is in
// flight (s.mu released), every other queue operation must proceed. The old
// lock-across-create design froze Get/List/Cancel for the whole ~2s create.
func TestPairingInProgressDoesNotBlockQueueOperations(t *testing.T) {
	service := NewService()
	creator := &slowMatchCreator{delay: 700 * time.Millisecond}
	service.SetMatchCreator(creator)

	// Seed one waiter with the SLOW creator wired: this enqueue will block
	// in its creator call for the delay, holding a pairing reservation.
	done := make(chan Ticket, 1)
	go func() {
		// First guest queues up (no opponent -> returns immediately, no
		// creator call). Do that synchronously first.
		if _, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "slow_waiter", 1200, "Waiter"); err != nil {
			t.Errorf("seed waiter: %v", err)
		}
		// Now a joiner pairs with the waiter: this call runs the creator
		// for 700ms with s.mu RELEASED.
		ticket, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "slow_joiner", 1205, "Joiner")
		if err != nil {
			t.Errorf("pairing enqueue: %v", err)
		}
		done <- ticket
	}()

	// Give the pairing goroutine time to reach the creator call (past the
	// reservation phase, which needs the lock).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := service.FindActiveTicket("slow_joiner", ""); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("joiner ticket never appeared; pairing reservation never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Wait until the reservation is actually in the creator call: the
	// ticket moves to pairing only while reserved, and the creator runs
	// after that. Poll for the room assignment on either ticket.
	sawPairing := false
	deadline = time.Now().Add(5 * time.Second)
	for !sawPairing {
		listed := service.List(QueueCasual, contracts.MatchModeOpenCards)
		for _, ticket := range listed {
			if ticket.AssignedRoom != "" {
				sawPairing = true
			}
		}
		if sawPairing || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !sawPairing {
		t.Fatal("pairing reservation never reached the creator phase")
	}

	// THE assertion: these must all complete while the creator is still
	// sleeping (~700ms). Under the old design each blocked ~700ms.
	opStart := time.Now()
	if _, ok := service.Get("nope"); ok {
		t.Fatal("Get(nope) must miss")
	}
	_ = service.List(QueueCasual, contracts.MatchModeOpenCards)
	_ = service.Snapshot(QueueCasual, contracts.MatchModeOpenCards)
	_, _, _ = service.Cancel("nope", "no-secret")
	opElapsed := time.Since(opStart)
	if opElapsed > 200*time.Millisecond {
		t.Fatalf("queue operations blocked %v during an in-flight pairing; s.mu was held across the creator call", opElapsed)
	}

	paired := <-done
	if paired.Status != StatusMatched {
		t.Fatalf("expected the pairing to complete as matched, got %s", paired.Status)
	}
	if len(creator.assignments) != 1 {
		t.Fatalf("expected exactly 1 match creation, got %d", len(creator.assignments))
	}
}

// A pairing reservation claims the waiter: a concurrent second enqueue must
// neither pair with the same waiter nor re-issue anything to it. The
// reservation IS the double-pairing guard.
func TestPairingReservationBlocksDoublePairing(t *testing.T) {
	service := NewService()
	creator := &slowMatchCreator{delay: 500 * time.Millisecond}
	service.SetMatchCreator(creator)

	if _, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "waiter_x", 1200, "Waiter"); err != nil {
		t.Fatalf("seed waiter: %v", err)
	}

	paired := make(chan Ticket, 1)
	go func() {
		ticket, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "joiner_1", 1205, "Joiner1")
		if err != nil {
			t.Errorf("joiner_1: %v", err)
		}
		paired <- ticket
	}()

	// Wait for the pairing to reach the creator phase.
	deadline := time.Now().Add(5 * time.Second)
	for {
		ticket, ok := service.FindActiveTicket("joiner_1", "")
		if ok && ticket.AssignedRoom != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("joiner_1 pairing never reached the creator phase")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A second joiner arrives while the waiter is RESERVED: it must NOT pair
	// with the reserved waiter -- it queues up instead (publicView shows it
	// as queued) -- and the waiter is still consumed exactly once.
	second, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "joiner_2", 1205, "Joiner2")
	if err != nil {
		t.Fatalf("joiner_2 must be accepted (queued), got %v", err)
	}
	if second.Status != StatusQueued {
		t.Fatalf("joiner_2 must wait (waiter is reserved), got %s", second.Status)
	}

	first := <-paired
	if first.Status != StatusMatched {
		t.Fatalf("joiner_1 must eventually match, got %s", first.Status)
	}
	// Exactly one creation for the one pairing.
	if len(creator.assignments) != 1 {
		t.Fatalf("expected exactly 1 match creation, got %d", len(creator.assignments))
	}
}

// A process crash mid-pairing leaves pairing reservations behind. The
// recovery sweep must roll them back to queued so both guests keep seeking.
func TestStalePairingReservationRecoversToQueued(t *testing.T) {
	service := NewService()
	// No creator: enqueue pairs inline via the nil-creator path, which we
	// avoid -- wire a creator that panics mid-call to simulate a crash
	// between reservation and completion. Recovery is exercised directly on
	// a hand-seeded reservation instead: deterministic, no goroutines.
	if _, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "gone_white", 1200, "White"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Hand-roll the reservation the crash would have left behind.
	ticket, _ := service.FindActiveTicket("gone_white", "")
	service.mu.Lock()
	reserved := ticket
	reserved.Status = StatusPairing
	reserved.AssignedRoom = "room_crashed"
	reserved.UpdatedAt = service.nowUTC().Add(-2 * time.Minute) // far past pairingRecoveryTTL
	service.tickets[reserved.TicketID] = reserved
	service.mu.Unlock()

	// Any queue operation triggers recovery first.
	fresh, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "late_joiner", 1210, "Late")
	if err != nil {
		t.Fatalf("enqueue after crash: %v", err)
	}
	// The stale reservation rolled back to queued; the late joiner should
	// have paired with it.
	if fresh.Status != StatusMatched {
		t.Fatalf("late joiner should pair with the recovered waiter, got %s", fresh.Status)
	}
	recovered, ok := service.Get(reserved.TicketID)
	if !ok || recovered.Status != StatusMatched {
		t.Fatalf("recovered waiter should be matched by the late joiner, got %#v", recovered)
	}
}
