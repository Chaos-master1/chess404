package match

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// CreateMatch builds its snapshots under c.mu, releases the lock, and used to
// call flushCommit(persistSnap, c.presence) -- marshalling the LIVE presence
// struct after the lock was gone. The broadcast worker takes the same
// container lock and rewrites presence.WhiteConnected and
// presence.DisconnectGraceFor on every sweep (evaluatePresenceRuntime), so a
// match created while the broadcaster was running could read those fields
// mid-write.
//
// CI caught it as `WARNING: DATA RACE` with the write at
// match_lifecycle.go:1290 against the read in buildRedisSaveBundle
// (persist_queue.go:98) inside TestRunGauntletProducesAMeasurement. Every
// other flushCommit/saveToRedis call site holds c.mu across the call; this
// test pins CreateMatch to that invariant by running the two goroutines
// directly: creations here, a tight collectAndBroadcast loop on the other
// side. Only meaningful under -race.
func TestCreateMatchFlushesPresenceUnderTheContainerLock(t *testing.T) {
	service := NewService()
	defer service.Close()

	stop := make(chan struct{})
	var sweeps sync.WaitGroup
	sweeps.Add(1)
	go func() {
		defer sweeps.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// Same work the 1s broadcast ticker does, run tight so the
				// window between CreateMatch's unlock and its presence
				// marshal is actually hit.
				service.collectAndBroadcast(time.Now().UTC())
			}
		}
	}()

	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		createTestMatch(service, contracts.CreateMatchRequest{
			MatchID:      fmt.Sprintf("flush_race_%02d", i),
			WhiteGuestID: "guest_white",
			BlackGuestID: "guest_black",
		}, now.Add(time.Duration(i)*time.Second))
	}

	close(stop)
	sweeps.Wait()
}
