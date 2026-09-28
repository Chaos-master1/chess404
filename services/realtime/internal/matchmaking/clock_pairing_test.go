package matchmaking

import (
	"path/filepath"
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

// Clock equality is part of the pairing lane identity: two seekers that agree
// on queue and mode but picked different time controls must wait for their own
// clock pool instead of pairing into a match neither of them chose.
func TestPairingRequiresSameClock(t *testing.T) {
	service := NewService()

	waiter, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_30m", 1200, "Thirty", "", 1800, 0)
	if err != nil {
		t.Fatalf("enqueue 30+0 waiter: %v", err)
	}
	if waiter.Status != StatusQueued {
		t.Fatalf("expected 30+0 seeker to wait for a same-clock opponent, got %s", waiter.Status)
	}
	if waiter.ClockSeconds != 1800 || waiter.ClockIncrement != 0 {
		t.Fatalf("unexpected waiter clock %d+%d", waiter.ClockSeconds, waiter.ClockIncrement)
	}

	fast, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_5m", 1195, "Blitz", "", 300, 0)
	if err != nil {
		t.Fatalf("enqueue 5+0 seeker: %v", err)
	}
	if fast.Status != StatusQueued {
		t.Fatalf("5+0 seeker must not pair with a 30+0 waiter, got %s", fast.Status)
	}

	same, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_30m_2", 1210, "ThirtyToo", "", 1800, 0)
	if err != nil {
		t.Fatalf("enqueue second 30+0 seeker: %v", err)
	}
	if same.Status != StatusMatched {
		t.Fatalf("expected same-clock pair to match, got %s", same.Status)
	}
	if same.ClockSeconds != 1800 || same.ClockIncrement != 0 {
		t.Fatalf("matched ticket lost its clock: %d+%d", same.ClockSeconds, same.ClockIncrement)
	}

	stillQueued, ok := service.Get(fast.TicketID)
	if !ok || stillQueued.Status != StatusQueued {
		t.Fatalf("5+0 ticket must still be waiting after the 30+0 pair, got %#v", stillQueued)
	}

	anotherFast, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_5m_2", 1205, "BlitzToo", "", 300, 0)
	if err != nil {
		t.Fatalf("enqueue second 5+0 seeker: %v", err)
	}
	if anotherFast.Status != StatusMatched || anotherFast.ClockSeconds != 300 {
		t.Fatalf("expected the two 5+0 seekers to pair, got status=%s clock=%d+%d", anotherFast.Status, anotherFast.ClockSeconds, anotherFast.ClockIncrement)
	}
}

// Increment participates in the same equality rule: 10+0 and 15+0 seekers in
// the same lane stay separate pools.
func TestPairingRequiresSameIncrement(t *testing.T) {
	service := NewService()

	inc, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_inc", 1500, "Inc", "", 600, 10)
	if err != nil {
		t.Fatalf("enqueue 10+0 seeker: %v", err)
	}
	noInc, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_noinc", 1500, "NoInc", "", 600, 0)
	if err != nil {
		t.Fatalf("enqueue 10+0 seeker: %v", err)
	}
	if inc.Status != StatusQueued || noInc.Status != StatusQueued {
		t.Fatalf("increment mismatch must not pair, got %s and %s", inc.Status, noInc.Status)
	}

	incToo, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_inc_2", 1505, "IncToo", "", 600, 10)
	if err != nil {
		t.Fatalf("enqueue second 10+0 seeker: %v", err)
	}
	if incToo.Status != StatusMatched || incToo.ClockIncrement != 10 {
		t.Fatalf("expected the two 10+0 seekers to pair, got status=%s inc=%d", incToo.Status, incToo.ClockIncrement)
	}
}

func TestNormalizeQueueClockValues(t *testing.T) {
	cases := []struct {
		in   int64
		want int64
	}{
		{0, defaultQueueClockSeconds},
		{-1, defaultQueueClockSeconds},
		{301, defaultQueueClockSeconds},
		{12000, defaultQueueClockSeconds},
		{300, 300},
		{600, 600},
		{900, 900},
		{1800, 1800},
		{3600, 3600},
	}
	for _, tc := range cases {
		if got := normalizeClockSeconds(tc.in); got != tc.want {
			t.Errorf("normalizeClockSeconds(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}

	incCases := []struct {
		in   int64
		want int64
	}{
		{-5, 0},
		{0, 0},
		{10, 10},
		{15, 15},
		{45, maxQueueClockIncrementSeconds},
	}
	for _, tc := range incCases {
		if got := normalizeClockIncrement(tc.in); got != tc.want {
			t.Errorf("normalizeClockIncrement(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// Unknown clocks never reach a ticket: they normalize to the default instead
// of letting a malformed payload inject an arbitrary control into a match.
func TestEnqueueNormalizesUnknownClockToDefault(t *testing.T) {
	service := NewService()

	ticket, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_default", 1200, "Defaults", "", 9999, 7)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if ticket.ClockSeconds != defaultQueueClockSeconds {
		t.Fatalf("unknown clock should normalize to default, got %d", ticket.ClockSeconds)
	}
	if ticket.ClockIncrement != 7 {
		t.Fatalf("in-range increment should be preserved, got %d", ticket.ClockIncrement)
	}
}

// The SQLite store must survive a restart with the cancel secret and the
// picked clock intact -- otherwise every queued client loses its cancel
// credential (401s on cancel) and matches come back at the wrong speed.
func TestSQLiteTicketStoreRoundTripKeepsClockAndCancelSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickets.db")

	store, err := newSQLiteTicketStore(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	service, err := newPersistentService(store)
	if err != nil {
		_ = store.close()
		t.Fatalf("build service: %v", err)
	}
	service.SetMatchCreator(&captureMatchCreator{})

	ticket, err := service.EnqueueWithAccount(QueueCasual, contracts.MatchModeOpenCards, "guest_persist", 1240, "Persist", "", 900, 10)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if ticket.Status != StatusQueued {
		t.Fatalf("expected a lone queued ticket, got %s", ticket.Status)
	}
	if ticket.CancelSecret == "" {
		t.Fatal("expected the create response to carry a cancel secret")
	}
	if ticket.ClockSeconds != 900 || ticket.ClockIncrement != 10 {
		t.Fatalf("unexpected created ticket clock %d+%d", ticket.ClockSeconds, ticket.ClockIncrement)
	}

	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	reopenStore, err := newSQLiteTicketStore(path)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	reloaded, err := newPersistentService(reopenStore)
	if err != nil {
		_ = reopenStore.close()
		t.Fatalf("rebuild service: %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	got, ok := reloaded.Get(ticket.TicketID)
	if !ok {
		t.Fatal("ticket missing after restart")
	}
	if got.CancelSecret != ticket.CancelSecret {
		t.Fatalf("cancel secret lost across restart: %q vs %q", got.CancelSecret, ticket.CancelSecret)
	}
	if got.ClockSeconds != 900 || got.ClockIncrement != 10 {
		t.Fatalf("clock lost across restart: %d+%d", got.ClockSeconds, got.ClockIncrement)
	}
	if got.Status != StatusQueued {
		t.Fatalf("unexpected reloaded status %s", got.Status)
	}
}
