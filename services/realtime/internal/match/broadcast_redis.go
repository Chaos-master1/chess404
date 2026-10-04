package match

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

type Broadcaster interface {
	Publish(matchID string, data []byte) error
	Subscribe(matchID string) <-chan []byte
	Unsubscribe(matchID string)
	Ping() error
	Close() error
}

// redisSubscription tracks a per-matchID Redis PubSub plus the count of
// active local subscribers. The pubsub is closed only when the last
// subscriber unsubscribes, so multiple concurrent Subscribe() calls for
// the same matchID share the same underlying connection safely.
type redisSubscription struct {
	ps       *redis.PubSub
	refCount int32
}

type RedisBroadcaster struct {
	client    *redis.Client
	keyPrefix string
	mu        sync.Mutex
	subs      map[string]*redisSubscription
	quit      chan struct{}
}

func NewRedisBroadcaster(redisURL, keyPrefix string) (*RedisBroadcaster, error) {
	if keyPrefix == "" {
		keyPrefix = "chess404:match"
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	// See the matching comment in platform/match_claims_redis.go: go-redis's
	// 3s default leaves no margin against a remote managed Redis and causes
	// spurious "Conn has unread data" pool churn under any latency spike.
	// This only affects the regular command connection pool (Publish etc.);
	// PubSub subscriptions use their own dedicated connection with their own
	// idle-read handling, unaffected by this client-level timeout.
	opts.DialTimeout = 10 * time.Second
	opts.ReadTimeout = 10 * time.Second
	opts.WriteTimeout = 10 * time.Second
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &RedisBroadcaster{
		client:    client,
		keyPrefix: keyPrefix,
		subs:      make(map[string]*redisSubscription),
		quit:      make(chan struct{}),
	}, nil
}

func (b *RedisBroadcaster) channelName(matchID string) string {
	return fmt.Sprintf("%s:%s:broadcast", b.keyPrefix, matchID)
}

func (b *RedisBroadcaster) Publish(matchID string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return b.client.Publish(ctx, b.channelName(matchID), data).Err()
}

func (b *RedisBroadcaster) Subscribe(matchID string) <-chan []byte {
	// Fast path: an existing subscription for this match -- bump the
	// refcount and fan out. No network I/O is ever done while holding
	// b.mu (see the slow-path comment for why that matters).
	b.mu.Lock()
	if sub, ok := b.subs[matchID]; ok {
		atomic.AddInt32(&sub.refCount, 1)
		ps := sub.ps
		b.mu.Unlock()
		return b.fanOut(matchID, ps)
	}
	b.mu.Unlock()

	// Slow path: dial the dedicated pub/sub connection OUTSIDE b.mu.
	// client.Subscribe performs a TCP+TLS handshake, which on a
	// cross-region managed Redis costs hundreds of milliseconds; holding
	// the global mutex across it serialized every concurrent match
	// creation behind one handshake at a time (observed in production as
	// a ~450ms-per-creation latency staircase that blew matchmaking's 3s
	// create timeout under burst pairing). Concurrent creators now dial
	// in parallel; a caller that loses the race closes its extra
	// connection and adopts the winner's subscription. A concurrent
	// Unsubscribe in the same window no-ops (it finds no map entry), and
	// the re-check under the lock prevents any use-after-close.
	ps := b.client.Subscribe(context.Background(), b.channelName(matchID))

	b.mu.Lock()
	sub, ok := b.subs[matchID]
	if ok {
		b.mu.Unlock()
		_ = ps.Close()
		atomic.AddInt32(&sub.refCount, 1)
		return b.fanOut(matchID, sub.ps)
	}
	sub = &redisSubscription{ps: ps, refCount: 1}
	b.subs[matchID] = sub
	b.mu.Unlock()

	return b.fanOut(matchID, sub.ps)
}

// fanOut returns a buffered channel fed by a dedicated goroutine draining
// the subscription's shared message channel. Note: go-redis's PubSub.Channel()
// returns the SAME Go channel on every call, so multiple Subscribe calls for
// one matchID drain a shared channel (messages are distributed between
// consumers, not duplicated). Production subscribes exactly once per match
// (ensureRedisRelay's relayStarted guard), so in practice each PubSub has a
// single consumer. The relay goroutine never blocks longer than a single
// message write: if the consumer is slow, the message is dropped (and logged)
// so the underlying channel stays drained.
func (b *RedisBroadcaster) fanOut(matchID string, ps *redis.PubSub) <-chan []byte {
	ch := make(chan []byte, 64)
	go func() {
		defer close(ch)
		for msg := range ps.Channel() {
			select {
			case ch <- []byte(msg.Payload):
			default:
				slog.Warn("broadcast channel full, dropping message", "matchId", matchID)
			}
		}
	}()

	return ch
}

func (b *RedisBroadcaster) Unsubscribe(matchID string) {
	b.mu.Lock()
	sub, ok := b.subs[matchID]
	if !ok {
		b.mu.Unlock()
		return
	}
	remaining := atomic.AddInt32(&sub.refCount, -1)
	if remaining > 0 {
		b.mu.Unlock()
		return
	}
	// Last subscriber left: close the underlying pubsub and remove from map.
	_ = sub.ps.Close()
	delete(b.subs, matchID)
	b.mu.Unlock()
}

func (b *RedisBroadcaster) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return b.client.Ping(ctx).Err()
}

func (b *RedisBroadcaster) Close() error {
	close(b.quit)
	b.mu.Lock()
	defer b.mu.Unlock()
	for matchID, sub := range b.subs {
		_ = sub.ps.Close()
		delete(b.subs, matchID)
	}
	return b.client.Close()
}
