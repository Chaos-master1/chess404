package platform

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRedisClaimKey = "chess404:platform:match-claims"

type redisClaimStore struct {
	client *redis.Client
	key    string
}

func newRedisClaimStore(redisURL, key string) (*redisClaimStore, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	// go-redis's 3s default read/write timeout leaves no margin against a
	// remote managed Redis (Upstash): a response that arrives just after the
	// client gives up lands on the connection as unread bytes, which the
	// pool's next health check reports as "Conn has unread data (not push
	// notification)" and discards -- one wasted connection (and, if the
	// caller doesn't retry, one failed request) per slow round trip.
	options.DialTimeout = 10 * time.Second
	options.ReadTimeout = 10 * time.Second
	options.WriteTimeout = 10 * time.Second
	client := redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	if key == "" {
		key = defaultRedisClaimKey
	}
	return &redisClaimStore{
		client: client,
		key:    key,
	}, nil
}

func (s *redisClaimStore) backend() string {
	return "redis"
}

func (s *redisClaimStore) load() (map[string]MatchSeatClaim, error) {
	values, err := s.client.HGetAll(context.Background(), s.key).Result()
	if err != nil {
		return nil, err
	}

	claims := make(map[string]MatchSeatClaim, len(values))
	for claimKey, raw := range values {
		var claim MatchSeatClaim
		if err := json.Unmarshal([]byte(raw), &claim); err != nil {
			return nil, err
		}
		claims[claimKey] = claim
	}
	return claims, nil
}

// saveOne writes a single claim field. Every MatchClaimStore mutation
// (Put, a token-based Get-and-consume, an expiry prune) used to call the old
// persist(entireMap) here, which did a full HDEL-the-whole-hash-then-rewrite
// on every single write regardless of how many claims actually changed --
// against a request-quota-limited managed Redis (Upstash free tier), that
// multiplies real traffic by the size of the whole claims table on every
// write instead of costing one command for the one field that changed.
func (s *redisClaimStore) saveOne(key string, claim MatchSeatClaim) error {
	encoded, err := json.Marshal(claim)
	if err != nil {
		return err
	}
	return s.client.HSet(context.Background(), s.key, key, string(encoded)).Err()
}

// deleteMany removes the given fields in one HDEL round trip.
func (s *redisClaimStore) deleteMany(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	args := make([]string, len(keys))
	copy(args, keys)
	return s.client.HDel(context.Background(), s.key, args...).Err()
}

func (s *redisClaimStore) close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
