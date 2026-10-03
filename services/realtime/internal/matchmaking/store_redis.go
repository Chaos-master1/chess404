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

func (s *redisTicketStore) load() (map[string]Ticket, error) {
	values, err := s.client.HGetAll(context.Background(), s.key).Result()
	if err != nil {
		return nil, err
	}

	tickets := make(map[string]Ticket, len(values))
	for ticketID, raw := range values {
		var ticket Ticket
		if err := json.Unmarshal([]byte(raw), &ticket); err != nil {
			return nil, err
		}
		tickets[ticketID] = ticket
	}
	return tickets, nil
}

func (s *redisTicketStore) persist(tickets map[string]Ticket) error {
	ctx := context.Background()

	existing, err := s.client.HKeys(ctx, s.key).Result()
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()

	stale := make(map[string]struct{}, len(existing))
	for _, k := range existing {
		stale[k] = struct{}{}
	}

	for ticketID, ticket := range tickets {
		delete(stale, ticketID)
		encoded, err := json.Marshal(ticket)
		if err != nil {
			return err
		}
		pipe.HSet(ctx, s.key, ticketID, string(encoded))
	}

	for id := range stale {
		pipe.HDel(ctx, s.key, id)
	}

	// Refresh the hash's TTL in the same round trip as the writes above.
	pipe.Expire(ctx, s.key, ticketsTTL)

	_, err = pipe.Exec(ctx)
	return err
}

func (s *redisTicketStore) close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
