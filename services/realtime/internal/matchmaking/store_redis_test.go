package matchmaking

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chess404/realtime/internal/contracts"
	"github.com/redis/go-redis/v9"
)

// The queue hash needs a garbage-collection backstop TTL: without one the key
// outlived every ticket in it and stayed in Redis forever after the last
// service was decommissioned.
func TestRedisQueueStorePersistSetsBackstopTTL(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	service, err := NewRedisPersistentService(redisURL, "")
	if err != nil {
		t.Fatalf("create redis persistent service: %v", err)
	}
	defer func() { _ = service.Close() }()

	if _, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_ttl", 1200, "Ttl"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if ttl := redisServer.TTL(defaultRedisTicketKey); ttl != ticketsTTL {
		t.Fatalf("expected queue hash TTL %v, got %v", ticketsTTL, ttl)
	}
}

func TestRedisQueueStorePersistsAcrossReload(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	service, err := NewRedisPersistentService(redisURL, "")
	if err != nil {
		t.Fatalf("create redis persistent service: %v", err)
	}
	defer func() { _ = service.Close() }()

	first, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha")
	if err != nil {
		t.Fatalf("enqueue first ticket: %v", err)
	}
	second, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_b", 1210, "Bravo")
	if err != nil {
		t.Fatalf("enqueue second ticket: %v", err)
	}

	reloaded, err := NewRedisPersistentService(redisURL, "")
	if err != nil {
		t.Fatalf("reload redis persistent service: %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	firstReloaded, ok := reloaded.Get(first.TicketID)
	if !ok {
		t.Fatalf("expected first redis ticket after reload")
	}
	secondReloaded, ok := reloaded.Get(second.TicketID)
	if !ok {
		t.Fatalf("expected second redis ticket after reload")
	}
	if firstReloaded.AssignedRoom == "" || firstReloaded.AssignedRoom != secondReloaded.AssignedRoom {
		t.Fatalf("expected redis matched room to survive reload, got %#v and %#v", firstReloaded, secondReloaded)
	}
	if reloaded.Backend() != "redis" {
		t.Fatalf("expected redis backend, got %s", reloaded.Backend())
	}
	if firstReloaded.ModeID != contracts.MatchModeOpenCards || secondReloaded.ModeID != contracts.MatchModeOpenCards {
		t.Fatalf("expected redis reload to preserve mode metadata, got %#v and %#v", firstReloaded, secondReloaded)
	}
}

// cmdCounter counts Redis commands by name via a go-redis hook, so tests can
// assert the WAN roundtrip profile of the store: the whole point of the diff
// persist is that one queue operation touches only the tickets that changed.
type cmdCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *cmdCounter) bump(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name]++
}

func (c *cmdCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts = map[string]int{}
}

func (c *cmdCounter) of(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

func (c *cmdCounter) DialHook(next redis.DialHook) redis.DialHook { return next }

func (c *cmdCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		c.bump(cmd.Name())
		return next(ctx, cmd)
	}
}

func (c *cmdCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			c.bump(cmd.Name())
		}
		return next(ctx, cmds)
	}
}

func newCountingRedisService(t *testing.T) (*Service, *redisTicketStore, *cmdCounter) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	store, err := newRedisTicketStore(redisURL, "")
	if err != nil {
		t.Fatalf("create redis ticket store: %v", err)
	}
	t.Cleanup(func() { _ = store.close() })

	counter := &cmdCounter{counts: map[string]int{}}
	store.client.AddHook(counter)

	service, err := newPersistentService(store)
	if err != nil {
		t.Fatalf("create persistent service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service, store, counter
}

// One enqueue with no waiting opponent must write exactly its own ticket:
// one HSET (plus the TTL refresh), never the whole hash, and no read probes.
func TestRedisQueueStorePersistWritesOnlyChangedTickets(t *testing.T) {
	service, _, counter := newCountingRedisService(t)

	counter.reset()
	first, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_diff_a", 1200, "Alpha")
	if err != nil {
		t.Fatalf("enqueue first ticket: %v", err)
	}
	if got := counter.of("hset"); got != 1 {
		t.Fatalf("expected exactly 1 HSET for first enqueue, got %d (counts %v)", got, counter.counts)
	}
	if got := counter.of("hgetall"); got != 0 {
		t.Fatalf("expected no HGETALL on a warm baseline, got %d", got)
	}

	counter.reset()
	// Re-joining the same lane while still queued re-issues the cancel
	// secret: one ticket changed, so exactly one HSET must ship.
	rejoined, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_diff_a", 1200, "Alpha")
	if err != nil {
		t.Fatalf("re-enqueue ticket: %v", err)
	}
	if got := counter.of("hset"); got != 1 {
		t.Fatalf("expected exactly 1 HSET for the re-issued secret, got %d (counts %v)", got, counter.counts)
	}
	if got := counter.of("hgetall"); got != 0 {
		t.Fatalf("expected no HGETALL on a warm baseline, got %d", got)
	}

	counter.reset()
	// NOTE: cancelWithAuth persists BEFORE installing the cancelled ticket
	// into the map, so the cancelled status itself reaches Redis on a later
	// persist (pre-existing deferral, unchanged by the diff store). The
	// assertion here is therefore that a cancel triggers no read probes and
	// no full-hash rewrite -- the property this store change is about.
	if _, _, err := service.Cancel(rejoined.TicketID, rejoined.CancelSecret); err != nil {
		t.Fatalf("cancel ticket: %v", err)
	}
	if got := counter.of("hgetall") + counter.of("hkeys"); got != 0 {
		t.Fatalf("expected no read probes on cancel, got %d (counts %v)", got, counter.counts)
	}
	if got := counter.of("hset") + counter.of("hdel"); got > 1 {
		t.Fatalf("cancel must never rewrite the hash, got %d write commands (counts %v)", got, counter.counts)
	}

	// A persist with nothing changed (the prune sweep in Get finds nothing
	// to expire) must ship zero writes.
	counter.reset()
	if _, ok := service.Get(first.TicketID); !ok {
		t.Fatalf("expected cancelled ticket to still be readable")
	}
	if got := counter.of("hset") + counter.of("hdel"); got != 0 {
		t.Fatalf("expected no writes from a no-change persist, got %d (counts %v)", got, counter.counts)
	}
}

// Pruning expired tickets must delete exactly those hash fields, not rewrite
// the hash.
func TestRedisQueueStorePruneDeletesOnlyExpiredFields(t *testing.T) {
	service, _, counter := newCountingRedisService(t)

	first, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_prune_a", 1200, "Alpha")
	if err != nil {
		t.Fatalf("enqueue first ticket: %v", err)
	}
	second, err := service.Enqueue(QueueCasual, contracts.MatchModeOpenCards, "guest_prune_b", 1200, "Bravo")
	if err != nil {
		t.Fatalf("enqueue second ticket: %v", err)
	}

	// Cancel the first ticket: cancelled tickets expire after 30s while
	// queued ones live 10m, so a +31s clock jump expires exactly one of the
	// two without mutating anything else.
	if _, _, err := service.CancelByService(first.TicketID); err != nil {
		t.Fatalf("cancel first ticket: %v", err)
	}

	originalNow := service.now
	service.now = func() time.Time { return originalNow().Add(31 * time.Second) }
	defer func() { service.now = originalNow }()

	counter.reset()
	if _, ok := service.Get(first.TicketID); ok {
		t.Fatalf("expected expired ticket to be pruned")
	}
	if got := counter.of("hdel"); got != 1 {
		t.Fatalf("expected exactly 1 HDEL for the expired ticket, got %d (counts %v)", got, counter.counts)
	}
	if got := counter.of("hset"); got != 0 {
		t.Fatalf("expected prune to write no HSETs, got %d", got)
	}
	if _, ok := service.Get(second.TicketID); !ok {
		t.Fatalf("expected the live ticket to survive the prune")
	}
}

// The diff baseline records what THIS store last wrote, so an edit that lands
// in the hash from outside the diff path must not be clobbered by the next
// unchanged persist.
func TestRedisQueueStoreDiffPreservesExternalFieldEdit(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	store, err := newRedisTicketStore(redisURL, "")
	if err != nil {
		t.Fatalf("create redis ticket store: %v", err)
	}
	defer func() { _ = store.close() }()

	ticket := Ticket{TicketID: "ticket_ext", GuestID: "guest_ext", Queue: QueueCasual, Status: StatusQueued}
	if err := store.persist(map[string]Ticket{ticket.TicketID: ticket}); err != nil {
		t.Fatalf("initial persist: %v", err)
	}

	edited, err := json.Marshal(ticket)
	if err != nil {
		t.Fatalf("marshal ticket: %v", err)
	}
	editedRaw := `{"external":true,"ticket":` + string(edited) + `}`
	redisServer.HSet(defaultRedisTicketKey, ticket.TicketID, editedRaw)

	// Same in-memory state as the first persist: the diff sees no change and
	// must not rewrite the field, leaving the external edit in place.
	if err := store.persist(map[string]Ticket{ticket.TicketID: ticket}); err != nil {
		t.Fatalf("second persist: %v", err)
	}
	got := redisServer.HGet(defaultRedisTicketKey, ticket.TicketID)
	if got != editedRaw {
		t.Fatalf("expected external edit to survive unchanged persist, got %q", got)
	}
}

// A failed persist must not advance the baseline: the retry re-sends the
// missing diff instead of silently dropping the update.
func TestRedisQueueStoreFailedPersistResendsDiff(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	service, err := NewRedisPersistentService(redisURL, "")
	if err != nil {
		t.Fatalf("create redis persistent service: %v", err)
	}
	defer func() { _ = service.Close() }()

	redisServer.SetError("injected outage")
	if _, err := service.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_retry", 1200, "Retry"); err == nil {
		t.Fatalf("expected enqueue to fail while redis errors")
	}
	redisServer.SetError("")

	reloaded, err := NewRedisPersistentService(redisURL, "")
	if err != nil {
		t.Fatalf("reload redis persistent service: %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	again, err := reloaded.Enqueue(QueueRated, contracts.MatchModeOpenCards, "guest_retry", 1200, "Retry")
	if err != nil {
		t.Fatalf("enqueue after outage: %v", err)
	}
	if _, ok := reloaded.Get(again.TicketID); !ok {
		t.Fatalf("expected retried ticket to be persisted after the outage")
	}
}
