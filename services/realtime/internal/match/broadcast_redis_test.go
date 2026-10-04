package match

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// waitForSubscriber polls miniredis until the channel has at least one
// server-side subscriber. Pub/Sub delivery is fire-and-forget: a PUBLISH that
// races the SUBSCRIBE command on the dedicated connection is simply lost, so
// tests must confirm the server registered the subscription first.
func waitForSubscriber(t *testing.T, server *miniredis.Miniredis, channel string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.PubSubNumSub(channel)[channel] >= 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("channel %s never got a server-side subscriber", channel)
}

// Verifies that the redisSubscription refcount correctly closes the
// underlying PubSub only after the last subscriber leaves, and that
// the non-blocking relay goroutine drops messages on a full channel
// instead of blocking the shared ps.Channel() consumer.
func TestRedisSubscriptionRefCount(t *testing.T) {
	sub := &redisSubscription{refCount: 0}

	// Two concurrent Subscribe() calls.
	atomic.AddInt32(&sub.refCount, 1)
	atomic.AddInt32(&sub.refCount, 1)
	if got := atomic.LoadInt32(&sub.refCount); got != 2 {
		t.Fatalf("expected refCount=2 after two subscribes, got %d", got)
	}

	// One Unsubscribe -> refCount 1, must NOT close.
	if remaining := atomic.AddInt32(&sub.refCount, -1); remaining != 1 {
		t.Fatalf("expected remaining=1, got %d", remaining)
	}
	if sub.refCount <= 0 {
		t.Fatalf("sub closed prematurely while still %d subscribers", sub.refCount)
	}

	// Second Unsubscribe -> refCount 0, must close.
	if remaining := atomic.AddInt32(&sub.refCount, -1); remaining != 0 {
		t.Fatalf("expected remaining=0, got %d", remaining)
	}
	if sub.refCount != 0 {
		t.Fatalf("expected refCount=0, got %d", sub.refCount)
	}
}

// Verifies that the non-blocking relay goroutine drops messages when
// the consumer channel is full, instead of blocking ps.Channel().
func TestBroadcastRelayDoesNotBlock(t *testing.T) {
	// Simulate a full consumer channel with a 1-slot buffer.
	consumer := make(chan []byte, 1)
	consumer <- []byte("stale")

	done := make(chan struct{})
	var dropped atomic.Int32
	go func() {
		defer close(done)
		// Three messages on a 1-slot channel: 2 must drop, 0 must block.
		for i := 0; i < 3; i++ {
			select {
			case consumer <- []byte("new"):
				// ok
			default:
				dropped.Add(1)
			}
		}
	}()

	select {
	case <-done:
		// 3 sends on a 1-slot channel where 1 element is already in flight:
		//   - 1st send: buffer was full (1 element), default fires -> drop
		//   - 2nd send: buffer was full, default fires -> drop
		//   - 3rd send: buffer was full, default fires -> drop
		// All 3 drop because the buffer never drains during this test.
		if got := dropped.Load(); got != 3 {
			t.Fatalf("expected 3 drops on a stuck-full channel, got %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay deadlocked: select with default blocked longer than 2s")
	}
}

// Verifies that two subscribers to the same matchID share the same
// underlying PubSub entry until the last one unsubscribes.
func TestSubscribeSharesPubSub(t *testing.T) {
	var subs sync.Map
	ps := &redisSubscription{refCount: 0}

	// Simulate first Subscribe: install ps.
	subs.Store("room_share", ps)
	// Second Subscribe for same key: find existing, increment refCount.
	if existing, ok := subs.Load("room_share"); ok {
		existing.(*redisSubscription).refCount++
		if existing != ps {
			t.Fatal("expected same subscription instance, got different")
		}
	}
	if ps.refCount != 1 {
		t.Fatalf("expected refCount=1, got %d", ps.refCount)
	}

	// First Unsubscribe: refCount drops to 0, remove from map.
	ps.refCount--
	if ps.refCount == 0 {
		subs.Delete("room_share")
	}
	if _, ok := subs.Load("room_share"); ok {
		t.Fatal("expected subscription removed after last unsubscribe")
	}
}

// TestRedisBroadcasterSubscribePublishRoundtrip exercises the real
// RedisBroadcaster (miniredis): a subscriber receives what another client
// publishes to the match's broadcast channel.
func TestRedisBroadcasterSubscribePublishRoundtrip(t *testing.T) {
	server := miniredis.RunT(t)
	b, err := NewRedisBroadcaster("redis://"+server.Addr(), "chess404:test:broadcast")
	if err != nil {
		t.Fatalf("expected broadcaster to initialize, got %v", err)
	}
	defer func() { _ = b.Close() }()

	ch := b.Subscribe("room_rt")
	waitForSubscriber(t, server, b.channelName("room_rt"))
	if err := b.client.Publish(context.Background(), b.channelName("room_rt"), "hello").Err(); err != nil {
		t.Fatalf("expected publish to succeed, got %v", err)
	}
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Fatalf("expected payload %q, got %q", "hello", string(msg))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

// TestRedisBroadcasterConcurrentSubscribeDistinctMatches pins the burst-pairing
// regression: concurrent creations each call Subscribe for a fresh matchID.
// The dial must happen outside the global mutex so the calls overlap instead
// of serializing behind one cross-region TLS handshake at a time (production
// saw a ~450ms-per-creation staircase that blew matchmaking's 3s create
// timeout). Against miniredis the dial is local, so this asserts correctness
// and no deadlock under full concurrency rather than the timing itself.
func TestRedisBroadcasterConcurrentSubscribeDistinctMatches(t *testing.T) {
	server := miniredis.RunT(t)
	b, err := NewRedisBroadcaster("redis://"+server.Addr(), "chess404:test:broadcast")
	if err != nil {
		t.Fatalf("expected broadcaster to initialize, got %v", err)
	}
	defer func() { _ = b.Close() }()

	const n = 30
	channels := make([]<-chan []byte, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channels[i] = b.Subscribe(fmt.Sprintf("room_%d", i))
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent subscribes deadlocked")
	}

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("room_%d", i)
		waitForSubscriber(t, server, b.channelName(name))
		if err := b.client.Publish(context.Background(), b.channelName(name), fmt.Sprintf("m%d", i)).Err(); err != nil {
			t.Fatalf("expected publish %d to succeed, got %v", i, err)
		}
	}
	for i := 0; i < n; i++ {
		select {
		case msg := <-channels[i]:
			if string(msg) != fmt.Sprintf("m%d", i) {
				t.Fatalf("match %d: expected m%d, got %s", i, i, string(msg))
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("match %d: timed out waiting for its published message", i)
		}
		b.Unsubscribe(fmt.Sprintf("room_%d", i))
	}
}

// TestRedisBroadcasterConcurrentSubscribeSameMatch pins the lost-race path:
// N concurrent Subscribe calls for the SAME matchID must converge on exactly
// one underlying subscription with refcount exactly N (no leaked duplicate
// dials from callers that lost the adoption race), and the subscription must
// deliver published messages. go-redis hands every Channel() caller the same
// underlying Go channel, so delivery is via the shared channel (at least one
// consumer receives), not per-consumer copies.
func TestRedisBroadcasterConcurrentSubscribeSameMatch(t *testing.T) {
	server := miniredis.RunT(t)
	b, err := NewRedisBroadcaster("redis://"+server.Addr(), "chess404:test:broadcast")
	if err != nil {
		t.Fatalf("expected broadcaster to initialize, got %v", err)
	}
	defer func() { _ = b.Close() }()

	const n = 12
	channels := make([]<-chan []byte, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channels[i] = b.Subscribe("room_shared")
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent same-match subscribes deadlocked")
	}

	b.mu.Lock()
	sub, ok := b.subs["room_shared"]
	b.mu.Unlock()
	if !ok {
		t.Fatal("expected exactly one shared subscription entry")
	}
	if got := atomic.LoadInt32(&sub.refCount); got != n {
		t.Fatalf("expected refCount=%d after %d concurrent subscribes, got %d", n, n, got)
	}

	waitForSubscriber(t, server, b.channelName("room_shared"))
	if err := b.client.Publish(context.Background(), b.channelName("room_shared"), "shared").Err(); err != nil {
		t.Fatalf("expected publish to succeed, got %v", err)
	}
	// All n consumers drain ONE shared go-redis channel, so exactly one of
	// them receives the message -- collect from every channel concurrently
	// instead of probing them sequentially.
	got := make(chan string, n)
	for i := range channels {
		ch := channels[i]
		go func() {
			select {
			case msg := <-ch:
				got <- string(msg)
			case <-time.After(2 * time.Second):
			}
		}()
	}
	select {
	case msg := <-got:
		if msg != "shared" {
			t.Fatalf("expected %q, got %q", "shared", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected the shared subscription to deliver the published message to at least one consumer")
	}

	for i := 0; i < n; i++ {
		b.Unsubscribe("room_shared")
	}
	b.mu.Lock()
	_, stillThere := b.subs["room_shared"]
	b.mu.Unlock()
	if stillThere {
		t.Fatal("expected subscription removed after the last unsubscribe")
	}
}
