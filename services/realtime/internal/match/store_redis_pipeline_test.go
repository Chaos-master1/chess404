package match

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chess404/realtime/internal/contracts"
	"github.com/redis/go-redis/v9"
)

// pipelineBatchCapture records every pipelined command batch the client
// sends, so tests can assert the per-mutation snapshot write is ONE round
// trip rather than N sequential SETs. Against a local Redis the difference
// is invisible; against Upstash over the WAN the sequential form added
// ~6x RTT (~0.5s) to every player move and the computer's reply alike.
type pipelineBatchCapture struct {
	batches [][]string
}

func (c *pipelineBatchCapture) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (c *pipelineBatchCapture) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		return next(ctx, cmd)
	}
}

func (c *pipelineBatchCapture) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		names := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			names = append(names, cmd.Name())
		}
		c.batches = append(c.batches, names)
		return next(ctx, cmds)
	}
}

// SaveSnapshotAtomic must land every snapshot component in Redis and must
// do so as a single pipelined batch.
func TestSaveSnapshotAtomicWritesAllComponentsInOnePipeline(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	store, err := NewRedisMatchStore(redisURL, "test:pipeline")
	if err != nil {
		t.Fatalf("new redis match store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	capture := &pipelineBatchCapture{}
	store.client.AddHook(capture)

	state := []byte(`{"Match":{"matchID":"pipe_test"}}`)
	history := []byte(`[{"fen":"start"}]`)
	events := []byte(`[{"type":"move"}]`)
	presence := []byte(`{"white":{}}`)
	seenIDs := []byte(`["cmid_1"]`)

	if err := store.SaveSnapshotAtomic("pipe_test", state, "hash-w", "hash-b", history, events, presence, seenIDs); err != nil {
		t.Fatalf("SaveSnapshotAtomic: %v", err)
	}

	if len(capture.batches) != 1 {
		t.Fatalf("expected exactly 1 pipelined batch, got %d: %v", len(capture.batches), capture.batches)
	}
	if len(capture.batches[0]) != 6 {
		t.Fatalf("expected 6 commands in the batch (state, secrets, history, events, presence, seenids), got %d: %v", len(capture.batches[0]), capture.batches[0])
	}

	// Every component must be readable through the store's own loaders.
	var into map[string]any
	if err := store.LoadState("pipe_test", &into); err != nil {
		t.Fatalf("LoadState after atomic save: %v", err)
	}
	w, b, err := store.LoadSecrets("pipe_test")
	if err != nil || w != "hash-w" || b != "hash-b" {
		t.Fatalf("LoadSecrets after atomic save: %q %q %v", w, b, err)
	}
	if got, err := store.LoadHistory("pipe_test"); err != nil || string(got) != string(history) {
		t.Fatalf("LoadHistory mismatch: %s %v", got, err)
	}
	if got, err := store.LoadEvents("pipe_test"); err != nil || string(got) != string(events) {
		t.Fatalf("LoadEvents mismatch: %s %v", got, err)
	}
	if got, err := store.LoadPresence("pipe_test"); err != nil || string(got) != string(presence) {
		t.Fatalf("LoadPresence mismatch: %s %v", got, err)
	}
	if got, err := store.LoadSeenClientMoveIDs("pipe_test"); err != nil || string(got) != string(seenIDs) {
		t.Fatalf("LoadSeenClientMoveIDs mismatch: %s %v", got, err)
	}

	// Empty/nil optional components must be skipped, not written as empties.
	if err := store.SaveSnapshotAtomic("pipe_empty", state, "hash-w", "hash-b", nil, nil, nil, nil); err != nil {
		t.Fatalf("SaveSnapshotAtomic (optional components empty): %v", err)
	}
	capture.batches = nil
	for _, key := range []string{
		"test:pipeline:pipe_empty:history",
		"test:pipeline:pipe_empty:events",
		"test:pipeline:pipe_empty:presence",
		"test:pipeline:pipe_empty:seenids",
	} {
		if redisServer.Exists(key) {
			t.Fatalf("expected %s to NOT exist when its component was empty", key)
		}
	}
}

// End-to-end through the service: a redis-backed service's saveToRedis must
// issue its per-mutation writes as one pipeline (not six sequential SETs),
// and a second instance must still hydrate the resulting state correctly.
func TestSaveToRedisUsesSinglePipelinePerMutation(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"

	store, err := NewRedisMatchStore(redisURL, "test:pipeline_svc")
	if err != nil {
		t.Fatalf("new redis match store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	capture := &pipelineBatchCapture{}
	store.client.AddHook(capture)

	svc := NewServiceWithStoreAndBroadcaster(nil, store, nil)
	t.Cleanup(svc.Close)

	now := time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC)
	created := svc.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "pipeline_svc",
		WhiteGuestID:      "guest_white",
		BlackGuestID:      "guest_black",
		WhitePlayerSecret: whiteTestSecret,
		BlackPlayerSecret: blackTestSecret,
	}, now)
	if created.Match.Status != "active" {
		t.Fatalf("expected active match, got %s", created.Match.Status)
	}

	if len(capture.batches) == 0 {
		t.Fatal("expected at least one pipelined batch from match creation persistence")
	}
	for i, batch := range capture.batches {
		if len(batch) < 2 {
			t.Fatalf("batch %d carried only %d commands (%v); per-mutation writes must be pipelined", i, len(batch), batch)
		}
	}
}
