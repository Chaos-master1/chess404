package match

import (
	"errors"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// The one-active-computer-game guard: a second CreateComputerMatch for a
// player with a live computer game must be rejected (mapped to 409 by the
// create route) instead of stacking an unjoinable game. The stale-owner edge
// matters: the in-memory map holds finished/evicted matches too, so the guard
// must only consider ACTIVE computer-mode containers.

func TestCreateComputerMatchBlocksSecondActiveGame(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "guard_first",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner",
	}, now)

	_, err := service.CreateComputerMatch(contracts.CreateMatchRequest{
		MatchID:      "guard_second",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner",
	}, now.Add(time.Second))
	if !errors.Is(err, ErrActiveComputerMatch) {
		t.Fatalf("expected ErrActiveComputerMatch for a second active computer game, got %v", err)
	}
}

func TestCreateComputerMatchAllowsAfterFinish(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "guard_done",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner2",
	}, now)

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "resign", MatchID: "guard_done", PlayerID: "white_player",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("resign: %v", err)
	}

	if _, err := service.CreateComputerMatch(contracts.CreateMatchRequest{
		MatchID:      "guard_next",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner2",
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected a new computer game after the previous one finished, got %v", err)
	}
}

func TestCreateComputerMatchIgnoresFinishedContainers(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "guard_finished_container",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner3",
	}, now)
	// Simulate the container lingering after the game ended (GC has a 30min
	// finished-TTL; the guard must not treat it as a live game).
	c := service.getMatchContainer("guard_finished_container")
	c.mu.Lock()
	c.state.Status = "finished"
	c.mu.Unlock()

	if _, err := service.CreateComputerMatch(contracts.CreateMatchRequest{
		MatchID:      "guard_next2",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner3",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("finished container must not block a new computer game, got %v", err)
	}
}

func TestCreateComputerMatchDoesNotTouchOtherModes(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	// A live hidden-cards game must not block a computer game (the guard is
	// computer-mode only), and non-computer creates bypass the guard entirely.
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "guard_hidden",
		ModeID:       contracts.MatchModeHiddenCards,
		WhiteGuestID: "guard_owner4",
	}, now)

	if _, err := service.CreateComputerMatch(contracts.CreateMatchRequest{
		MatchID:      "guard_comp",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guard_owner4",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("computer create must not be blocked by a non-computer game, got %v", err)
	}

	// Two hidden-cards creates are pairwise private rooms, allowed by design.
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "guard_hidden2",
		ModeID:       contracts.MatchModeHiddenCards,
		WhiteGuestID: "guard_owner4",
	}, now.Add(2*time.Second))
}

// The zombie GC: an ACTIVE match idle past the abandon TTL must be finalized
// as a draw-abandon and flushed to the archive before the container is
// evicted, instead of lingering "active" in the public feed forever. The
// observable contract is the archive upsert (the GC deletes the in-memory
// container right after finalizing).
func TestZombieActiveMatchIsFinalizedAsAbandon(t *testing.T) {
	archive := &captureArchiver{}
	service := NewServiceWithArchive(archive)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	snap := createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "zombie_1",
		WhiteGuestID: "zombie_white",
		BlackGuestID: "zombie_black",
	}, now)
	if snap.Match.Status != "active" {
		t.Fatalf("expected the created match to be active, got %q", snap.Match.Status)
	}

	// Advance past the 10-minute abandon TTL without any presence activity.
	later := now.Add(20 * time.Minute)
	service.gcFinishedMatches(later)

	var last contracts.MatchSnapshotResponse
	found := false
	for _, s := range archive.snapshots {
		if s.Match.MatchID == "zombie_1" {
			last = s
			found = true
		}
	}
	if !found {
		t.Fatal("zombie match was never archived")
	}
	if last.Match.Status != "finished" || (last.Match.FinishReason != "abandon" && last.Match.FinishReason != "abort") {
		t.Fatalf("expected zombie to finalize as draw/abandon or abort, got status=%q winner=%q reason=%q", last.Match.Status, last.Match.Winner, last.Match.FinishReason)
	}
}

func TestZombieGCIgnoresFreshMatches(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{MatchID: "zombie_fresh"}, now)
	service.gcFinishedMatches(now.Add(5 * time.Minute))

	c := service.getMatchContainer("zombie_fresh")
	if c == nil {
		t.Fatal("fresh active match must survive the sweep")
	}
	c.mu.Lock()
	status := c.state.Status
	c.mu.Unlock()
	if status != "active" {
		t.Fatalf("fresh active match must stay active, got %q", status)
	}
}

// Untimed matches and long thinks legitimately go >10 minutes without any
// mutation, so UpdatedAt alone must never trigger the zombie finalize. A
// connected player's presence heartbeat is the liveness signal: this match
// was idle 20 minutes on the wall clock but both players were heartbeating.
// Before the presence gate, this exact sequence draw-abandoned a live game.
func TestZombieGCKeepsHeartbeatedMatchAlive(t *testing.T) {
	archive := &captureArchiver{}
	service := NewServiceWithArchive(archive)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "zombie_alive",
		WhiteGuestID: "alive_white",
		BlackGuestID: "alive_black",
		ClockSeconds: 0, // untimed: nothing refreshes UpdatedAt during a think
	}, now)

	// Both players heartbeat at creation and again 20 minutes later (the
	// moment the GC runs) -- they are connected and thinking.
	if err := service.HeartbeatPresence("zombie_alive", testPresence("alive_white"), now); err != nil {
		t.Fatalf("white heartbeat: %v", err)
	}
	if err := service.HeartbeatPresence("zombie_alive", testPresence("alive_black"), now); err != nil {
		t.Fatalf("black heartbeat: %v", err)
	}
	later := now.Add(20 * time.Minute)
	if err := service.HeartbeatPresence("zombie_alive", testPresence("alive_white"), later); err != nil {
		t.Fatalf("late white heartbeat: %v", err)
	}
	if err := service.HeartbeatPresence("zombie_alive", testPresence("alive_black"), later); err != nil {
		t.Fatalf("late black heartbeat: %v", err)
	}

	service.gcFinishedMatches(later.Add(time.Second))

	c := service.getMatchContainer("zombie_alive")
	if c == nil {
		t.Fatal("heartbeat-kept match was evicted by the zombie GC")
	}
	c.mu.Lock()
	status := c.state.Status
	c.mu.Unlock()
	if status != "active" {
		t.Fatalf("heartbeat-kept match must stay active, got %q", status)
	}
	for _, snap := range archive.snapshots {
		if snap.Match.MatchID == "zombie_alive" && snap.Match.Status == "finished" {
			t.Fatal("heartbeat-kept match was finalized as abandoned")
		}
	}
}

// One live player is enough: an opponent ghost (no heartbeat) must not
// condemn a match whose other seat is still connected.
func TestZombieGCKeepsMatchAliveWhileOnePlayerConnected(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "zombie_one_alive",
		WhiteGuestID: "one_white",
		BlackGuestID: "one_black",
	}, now)
	if err := service.HeartbeatPresence("zombie_one_alive", testPresence("one_white"), now); err != nil {
		t.Fatalf("white heartbeat: %v", err)
	}
	// White is still connected at GC time; black's ghost never heartbeats
	// again after creation.
	later := now.Add(20 * time.Minute)
	if err := service.HeartbeatPresence("zombie_one_alive", testPresence("one_white"), later); err != nil {
		t.Fatalf("late white heartbeat: %v", err)
	}

	service.gcFinishedMatches(later.Add(time.Second))

	c := service.getMatchContainer("zombie_one_alive")
	if c == nil {
		t.Fatal("match with one connected player was evicted by the zombie GC")
	}
	c.mu.Lock()
	status := c.state.Status
	c.mu.Unlock()
	if status != "active" {
		t.Fatalf("match with one connected player must stay active, got %q", status)
	}
}

// The original zombie cleanup must still work when heartbeats stop: a match
// where NEITHER player has been heard from is finalized as an abandon draw.
func TestZombieGCFinalizesWhenHeartbeatsStop(t *testing.T) {
	archive := &captureArchiver{}
	service := NewServiceWithArchive(archive)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "zombie_stopped",
		WhiteGuestID: "stopped_white",
		BlackGuestID: "stopped_black",
	}, now)
	// Heartbeats happen once, then both players vanish.
	if err := service.HeartbeatPresence("zombie_stopped", testPresence("stopped_white"), now); err != nil {
		t.Fatalf("white heartbeat: %v", err)
	}
	if err := service.HeartbeatPresence("zombie_stopped", testPresence("stopped_black"), now); err != nil {
		t.Fatalf("black heartbeat: %v", err)
	}

	service.gcFinishedMatches(now.Add(20 * time.Minute))

	var last contracts.MatchSnapshotResponse
	found := false
	for _, s := range archive.snapshots {
		if s.Match.MatchID == "zombie_stopped" {
			last = s
			found = true
		}
	}
	if !found {
		t.Fatal("abandoned match was never archived")
	}
	if last.Match.Status != "finished" || (last.Match.FinishReason != "abandon" && last.Match.FinishReason != "abort") {
		t.Fatalf("expected abandoned match to finalize, got status=%q reason=%q", last.Match.Status, last.Match.FinishReason)
	}
}
