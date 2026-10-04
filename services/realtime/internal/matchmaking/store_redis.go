package matchmaking

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRedisTicketKey = "chess404:matchmaking:tickets"

// ticketsTTL is a garbage-collection backstop for the whole queue hash, not a
// liveness bound: every persist refreshes it, so a running service never
// loses a ticket to it. It only removes the hash once the queue has seen no
// write for a week -- comfortably longer than any ticket's lifetime (queued
// 10m, matched 15m, pairing 1m, cancelled 30s) -- so an abandoned deployment
// cannot leave an immortal key behind.
const ticketsTTL = 7 * 24 * time.Hour

type redisTicketStore struct {
	client *redis.Client
	key    string
	// lastPersisted mirrors the encoded ticket fields this store last
	// successfully wrote to (or loaded from) the hash. persist() diffs the
	// incoming map against it and ships only the changed fields. The old
	// implementation re-wrote EVERY ticket (an HKeys probe plus a full HSet
	// pipeline) on every queue operation, so with N live tickets a single
	// enqueue moved O(N) bytes over the WAN Redis and -- serialized under
	// the Service mutex -- convoyed every other queue operation behind it;
	// under a 20-pair soak that measured as 24-44s enqueues and 8s polls.
	// The Service only calls persist while holding s.mu, so the cache has
	// exactly one writer and needs no lock of its own. The cache advances
	// only after a successful Exec: a failed persist leaves it stale, so
	// the next persist re-sends the missing diff (no lost update).
	lastPersisted map[string]string
}

func newRedisTicketStore(redisURL, key string) (*redisTicketStore, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	// See the matching comment in platform/match_claims_redis.go: go-redis's
	// 3s default leaves no margin against a remote managed Redis and causes
	// spurious "Conn has unread data" pool churn under any latency spike.
	options.DialTimeout = 10 * time.Second
	options.ReadTimeout = 10 * time.Second
	options.WriteTimeout = 10 * time.Second
	client := redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	if key == "" {
		key = defaultRedisTicketKey
	}
	return &redisTicketStore{
		client: client,
		key:    key,
	}, nil
}

func (s *redisTicketStore) backend() string {
	return "redis"
}

// refreshCache syncs lastPersisted from the server without decoding tickets.
// persist uses it when it runs before any load (cold store): the diff must
// reflect what is actually in the hash, or a ticket written by an earlier
// process would be treated as new and one written by nobody would be kept.
func (s *redisTicketStore) refreshCache(ctx context.Context) error {
	values, err := s.client.HGetAll(ctx, s.key).Result()
	if err != nil {
		return err
	}
	cache := make(map[string]string, len(values))
	for ticketID, raw := range values {
		cache[ticketID] = raw
	}
	s.lastPersisted = cache
	return nil
}

func (s *redisTicketStore) load() (map[string]Ticket, error) {
	ctx := context.Background()
	values, err := s.client.HGetAll(ctx, s.key).Result()
	if err != nil {
		return nil, err
	}
	cache := make(map[string]string, len(values))
	tickets := make(map[string]Ticket, len(values))
	for ticketID, raw := range values {
		cache[ticketID] = raw
		var ticket Ticket
		if err := json.Unmarshal([]byte(raw), &ticket); err != nil {
			return nil, err
		}
		tickets[ticketID] = ticket
	}
	s.lastPersisted = cache
	return tickets, nil
}

func (s *redisTicketStore) persist(tickets map[string]Ticket) error {
	ctx := context.Background()
	if s.lastPersisted == nil {
		// Cold store: seed the diff baseline from the server first so this
		// and every later persist write only real changes.
		if err := s.refreshCache(ctx); err != nil {
			return err
		}
	}

	pipe := s.client.Pipeline()
	upserts := make(map[string]string)
	var deletes []string
	for ticketID, ticket := range tickets {
		encoded, err := json.Marshal(ticket)
		if err != nil {
			return err
		}
		raw := string(encoded)
		if prev, ok := s.lastPersisted[ticketID]; ok && prev == raw {
			continue
		}
		pipe.HSet(ctx, s.key, ticketID, raw)
		upserts[ticketID] = raw
	}

	for ticketID := range s.lastPersisted {
		if _, ok := tickets[ticketID]; !ok {
			pipe.HDel(ctx, s.key, ticketID)
			deletes = append(deletes, ticketID)
		}
	}

	// Refresh the hash's TTL in the same round trip as the writes above.
	pipe.Expire(ctx, s.key, ticketsTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	// Commit the baseline only now that the server acknowledged the writes;
	// on error it stays untouched and the next persist re-sends the diff.
	for ticketID, raw := range upserts {
		s.lastPersisted[ticketID] = raw
	}
	for _, ticketID := range deletes {
		delete(s.lastPersisted, ticketID)
	}
	return nil
}

func (s *redisTicketStore) close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
