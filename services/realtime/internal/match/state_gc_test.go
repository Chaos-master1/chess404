package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression test for a self-deadlock in gcFinishedMatches.
//
// matchMap.Range holds the shard's RLock across the callback, and
// matchMap.Delete takes that same shard's write lock. Calling Delete from
// inside Range therefore deadlocked the GC goroutine permanently and left the
// shard's RWMutex held for reading with a writer queued -- which blocks every
// subsequent RLock too. The observable effect was Stats() hanging forever,
// /api/system/status never returning, and every match hashing to that shard
// becoming unreachable.
func TestGCFinishedMatchesDoesNotDeadlock(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	// Enough matches that several distinct shards are exercised.
	ids := []string{"gc_a", "gc_b", "gc_c", "gc_d", "gc_e", "gc_f", "gc_g", "gc_h"}
	for _, id := range ids {
		createTestMatch(service, contracts.CreateMatchRequest{MatchID: id}, now)
		c := service.getMatchContainer(id)
		c.mu.Lock()
		c.state.Status = "finished"
		c.state.UpdatedAt = now
		c.mu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		service.gcFinishedMatches(now.Add(31 * time.Minute))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("gcFinishedMatches deadlocked (Delete called while Range holds the shard RLock)")
	}

	// The shard mutexes must still be usable after the GC pass. Before the fix
	// this call blocked forever even though the GC goroutine had "finished".
	stats := make(chan ServiceStats, 1)
	go func() { stats <- service.Stats() }()

	select {
	case got := <-stats:
		if got.LoadedMatches != 0 {
			t.Fatalf("expected all expired matches evicted, still loaded: %d", got.LoadedMatches)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stats() blocked after GC -- a shard mutex was left permanently locked")
	}
}

// Matches that have not yet aged past their TTL must survive the sweep.
func TestGCFinishedMatchesKeepsFreshMatches(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{MatchID: "gc_fresh"}, now)
	c := service.getMatchContainer("gc_fresh")
	c.mu.Lock()
	c.state.Status = "finished"
	c.state.UpdatedAt = now
	c.mu.Unlock()

	service.gcFinishedMatches(now.Add(5 * time.Minute))

	if _, ok := service.matches.Load("gc_fresh"); !ok {
		t.Fatal("match evicted before its TTL elapsed")
	}
}
