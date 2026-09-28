package match

import (
	"sync"
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

// Regression test for the finalizeAbandonedMatch data race.
//
// finalizeAbandonedMatch used to release c.mu BEFORE calling flushCommit and
// broadcastLocked. broadcastLocked -> deliverToSubscribersLocked reads c.subs
// (a map) and c.seqNum without the lock, so a player reconnecting exactly as
// the zombie GC finalized their match produced a concurrent map read+write --
// a fatal runtime panic that killed the whole match-service process, not just
// one request. Run under -race, this test hammers Subscribe/ApplyIntent while
// the GC finalizes an abandoned match and fails if the two ever race.
func TestGCFinalizeAbandonedDoesNotRaceSubscribers(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{MatchID: "zombie_race", Queue: "casual"}, now)
	c := service.getMatchContainer("zombie_race")
	c.mu.Lock()
	c.state.Status = "active"
	// Older than the activeAbandonTTL so the GC treats it as a zombie, and
	// presence-less so the legacy wall-clock rule applies (zombiePresenceLocked
	// returns true for containers without presence tracking).
	c.presence = nil
	c.state.UpdatedAt = now
	c.mu.Unlock()

	const workers = 8
	const iterations = 200
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Concurrent reconnect storm: each worker repeatedly subscribes (taking
	// c.mu and writing c.subs) and unsubscribes. Without the fix this is the
	// racing writer against the GC's unlocked deliverToSubscribersLocked.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-stop:
					return
				default:
				}
				_, unsubscribe, _, err := service.Subscribe("zombie_race", "", "")
				if err == nil && unsubscribe != nil {
					unsubscribe()
				}
			}
		}()
	}

	// The GC pass finalizes the zombie while subscribers churn.
	service.gcFinishedMatches(now.Add(11 * time.Minute))
	close(stop)
	wg.Wait()

	c.mu.Lock()
	status := c.state.Status
	c.mu.Unlock()
	if status != "finished" {
		t.Fatalf("expected zombie match finalized as finished, got %q", status)
	}
}
