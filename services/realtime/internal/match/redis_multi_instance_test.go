package match

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chess404/realtime/internal/contracts"
)

// These tests exercise the cross-instance wiring against a real (fake)
// Redis, not the local matchMap shortcuts the rest of this package's tests
// use. They cover exactly what changed: a match created on one process must
// be readable, joinable, and playable from a second process that shares only
// Redis, and both processes' subscribers must see every broadcast regardless
// of which process produced it.
//
// This does NOT prove match-service is safe to run at more than one replica.
// There is no per-match request routing (no consistent-hash/sharding layer
// exists in this codebase despite ARCHITECTURE.md describing one) and no
// distributed lock, so two instances that both receive a mutation for the
// same live match will each apply it to their own independent in-memory copy
// and each persist/broadcast their own divergent view. What these tests
// guarantee is: an instance that has to cold-load a match (because its own
// process restarted, or because it never held it) gets a correct, secret-
// authenticated view, and read-side fan-out (spectators, reconnects) is
// symmetric across instances. Match-service must stay at one replica until
// concurrent-write safety is addressed separately.

func newRedisBackedServiceForTest(t *testing.T, redisURL, keyPrefix string) *Service {
	t.Helper()
	store, err := NewRedisMatchStore(redisURL, keyPrefix)
	if err != nil {
		t.Fatalf("new redis match store: %v", err)
	}
	broadcaster, err := NewRedisBroadcaster(redisURL, keyPrefix)
	if err != nil {
		t.Fatalf("new redis broadcaster: %v", err)
	}
	svc := NewServiceWithStoreAndBroadcaster(nil, store, broadcaster)
	t.Cleanup(func() {
		svc.Close()
		_ = store.Close()
		_ = broadcaster.Close()
	})
	return svc
}

// A match created on one instance must be readable and playable from a
// second instance that never saw it locally -- the scenario that was
// previously impossible: every request had to land on the exact process
// that created the match, or it 404'd.
func TestCrossInstanceHydrationPreservesAuth(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	instanceA := newRedisBackedServiceForTest(t, redisURL, "test:cross_a")
	instanceB := newRedisBackedServiceForTest(t, redisURL, "test:cross_a")

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	created := instanceA.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "cross_a",
		WhiteGuestID:      "guest_white",
		BlackGuestID:      "guest_black",
		WhitePlayerSecret: whiteTestSecret,
		BlackPlayerSecret: blackTestSecret,
	}, now)
	if created.Match.Status != "active" {
		t.Fatalf("expected active match, got %s", created.Match.Status)
	}

	// instanceB never saw this match locally; it must hydrate from Redis.
	viewed, err := instanceB.GetMatchForViewer("cross_a", "guest_black", blackTestSecret)
	if err != nil {
		t.Fatalf("instanceB GetMatchForViewer: %v", err)
	}
	if len(viewed.Match.BlackHand) == 0 {
		t.Fatal("instanceB should see black's own hand after hydration")
	}
	if viewed.Match.WhitePlayerSecret != "" || viewed.Match.BlackPlayerSecret != "" {
		t.Fatal("hydrated snapshot leaked a seat secret to the viewer")
	}

	// The secret must still authenticate on instanceB: apply an intent there.
	resp, err := instanceB.ApplyIntent(contracts.PlayerIntent{
		Type:         "resign",
		MatchID:      "cross_a",
		PlayerID:     "guest_black",
		PlayerSecret: blackTestSecret,
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("instanceB should be able to apply an authenticated intent after hydration: %v", err)
	}
	if resp.Match.Status != "finished" {
		t.Fatalf("expected resign to finish the match, got status=%s", resp.Match.Status)
	}
}

// A snapshot broadcast by whichever instance handles a mutation must reach a
// subscriber whose connection landed on a different instance -- in either
// direction (the instance that created the match, and one that only
// hydrated it).
func TestCrossInstanceBroadcastReachesRelayedSubscriber(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	instanceA := newRedisBackedServiceForTest(t, redisURL, "test:cross_b")
	instanceB := newRedisBackedServiceForTest(t, redisURL, "test:cross_b")

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	instanceA.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "cross_b",
		WhiteGuestID:      "guest_white",
		BlackGuestID:      "guest_black",
		WhitePlayerSecret: whiteTestSecret,
		BlackPlayerSecret: blackTestSecret,
	}, now)

	// A spectator connects through instance B, which must hydrate first --
	// that hydration is what starts B's relay subscription for this matchID.
	streamB, unsubB, _, err := instanceB.Subscribe("cross_b", "", "")
	if err != nil {
		t.Fatalf("instanceB Subscribe: %v", err)
	}
	defer unsubB()

	// The relay's underlying redis.PubSub.Subscribe happens asynchronously
	// relative to Subscribe() returning; give it a moment before A publishes.
	time.Sleep(150 * time.Millisecond)

	if _, err := instanceA.ApplyIntent(contracts.PlayerIntent{
		Type:         "resign",
		MatchID:      "cross_b",
		PlayerID:     "guest_white",
		PlayerSecret: whiteTestSecret,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("instanceA ApplyIntent: %v", err)
	}

	select {
	case snapshot := <-streamB:
		if snapshot.Match.Status != "finished" {
			t.Fatalf("expected relayed snapshot to show finished match, got %s", snapshot.Match.Status)
		}
		if snapshot.Match.WhitePlayerSecret != "" || snapshot.Match.BlackPlayerSecret != "" {
			t.Fatal("relayed snapshot leaked a seat secret")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("spectator on instanceB never received instanceA's broadcast via the redis relay")
	}
}

// seqNum must be a globally monotonic counter shared across instances, not a
// per-process counter that would restart or diverge depending on which
// instance a client's reconnect happens to land on.
func TestSeqNumIsMonotonicAcrossInstances(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	instanceA := newRedisBackedServiceForTest(t, redisURL, "test:cross_seq")
	instanceB := newRedisBackedServiceForTest(t, redisURL, "test:cross_seq")

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	instanceA.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "cross_seq",
		WhiteGuestID:      "guest_white",
		BlackGuestID:      "guest_black",
		WhitePlayerSecret: whiteTestSecret,
		BlackPlayerSecret: blackTestSecret,
	}, now)

	streamA, unsubA, _, err := instanceA.Subscribe("cross_seq", "guest_white", whiteTestSecret)
	if err != nil {
		t.Fatalf("instanceA Subscribe: %v", err)
	}
	defer unsubA()

	if _, err := instanceA.ApplyIntent(contracts.PlayerIntent{
		Type: "send_chat", MatchID: "cross_seq", PlayerID: "guest_white",
		PlayerSecret: whiteTestSecret, Text: "hi",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("instanceA chat intent: %v", err)
	}

	var seqFromA int64
	select {
	case snap := <-streamA:
		seqFromA = snap.SeqNum
	case <-time.After(2 * time.Second):
		t.Fatal("instanceA's own subscriber never received its own broadcast")
	}
	if seqFromA == 0 {
		t.Fatal("expected a nonzero seq from instanceA's own broadcast")
	}

	time.Sleep(150 * time.Millisecond) // let instanceB's relay subscription settle

	// instanceB hydrates the same match and issues its own mutation. If seq
	// were still a per-process counter, this would restart at 1 on instanceB
	// and instanceA's subscriber would see it go backwards.
	if _, err := instanceB.ApplyIntent(contracts.PlayerIntent{
		Type: "send_chat", MatchID: "cross_seq", PlayerID: "guest_black",
		PlayerSecret: blackTestSecret, Text: "hey",
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("instanceB chat intent: %v", err)
	}

	select {
	case snap := <-streamA:
		if snap.SeqNum <= seqFromA {
			t.Fatalf("expected seq to keep increasing across instances: instanceA=%d then instanceB=%d", seqFromA, snap.SeqNum)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("instanceA's subscriber never received instanceB's relayed broadcast")
	}
}
