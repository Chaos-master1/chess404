package match

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type MatchStore interface {
	SaveState(matchID string, snapshot any) error
	LoadState(matchID string, into any) error
	SaveHistory(matchID string, history []byte) error
	LoadHistory(matchID string) ([]byte, error)
	SaveEvents(matchID string, events []byte) error
	LoadEvents(matchID string) ([]byte, error)
	SavePresence(matchID string, presence []byte) error
	LoadPresence(matchID string) ([]byte, error)
	// SaveSnapshotAtomic persists all per-mutation snapshot components in a
	// single pipelined round trip. Optional components (history, events,
	// presence, seenIDs) are skipped when nil/empty. Implementations that
	// do not batch may fall back to the individual Save* calls in order.
	SaveSnapshotAtomic(matchID string, state []byte, secretWhite, secretBlack string, history, events, presence, seenIDs []byte) error
	IncSeq(matchID string) (int64, error)
	LoadSeq(matchID string) (int64, error)
	// LoadHydrationBundle returns every value a container rebuild reads --
	// state, presence, seen client move IDs and the seq counter -- in one
	// round trip. Missing keys are nil / zero, not errors; only transport
	// failures are. Hydration holds the service's global lock, so N
	// sequential GETs there stalled every match operation on the instance.
	LoadHydrationBundle(matchID string) (state, presence, seenIDs []byte, seq int64, err error)
	DeleteMatch(matchID string) error
	SaveSeenClientMoveIDs(matchID string, ids []byte) error
	LoadSeenClientMoveIDs(matchID string) ([]byte, error)
	Ping() error
	Close() error
}

type RedisMatchStore struct {
	client    *redis.Client
	keyPrefix string
}

func NewRedisMatchStore(redisURL, keyPrefix string) (*RedisMatchStore, error) {
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
	opts.DialTimeout = 10 * time.Second
	opts.ReadTimeout = 10 * time.Second
	opts.WriteTimeout = 10 * time.Second
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &RedisMatchStore{client: client, keyPrefix: keyPrefix}, nil
}

func (s *RedisMatchStore) stateKey(matchID string) string {
	return fmt.Sprintf("%s:%s:state", s.keyPrefix, matchID)
}

func (s *RedisMatchStore) secretsKey(matchID string) string {
	return fmt.Sprintf("%s:%s:secrets", s.keyPrefix, matchID)
}

func (s *RedisMatchStore) historyKey(matchID string) string {
	return fmt.Sprintf("%s:%s:history", s.keyPrefix, matchID)
}

func (s *RedisMatchStore) eventsKey(matchID string) string {
	return fmt.Sprintf("%s:%s:events", s.keyPrefix, matchID)
}

func (s *RedisMatchStore) presenceKey(matchID string) string {
	return fmt.Sprintf("%s:%s:presence", s.keyPrefix, matchID)
}

func (s *RedisMatchStore) seenIDsKey(matchID string) string {
	return fmt.Sprintf("%s:%s:seenids", s.keyPrefix, matchID)
}

func (s *RedisMatchStore) seqKey(matchID string) string {
	return fmt.Sprintf("%s:%s:seq", s.keyPrefix, matchID)
}

const matchTTL = 1 * time.Hour
const presenceTTL = 5 * time.Minute

func (s *RedisMatchStore) SaveState(matchID string, snapshot any) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	ctx := context.Background()
	return s.client.Set(ctx, s.stateKey(matchID), data, matchTTL).Err()
}

func (s *RedisMatchStore) LoadState(matchID string, into any) error {
	ctx := context.Background()
	data, err := s.client.Get(ctx, s.stateKey(matchID)).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func (s *RedisMatchStore) SaveHistory(matchID string, history []byte) error {
	ctx := context.Background()
	return s.client.Set(ctx, s.historyKey(matchID), history, matchTTL).Err()
}

func (s *RedisMatchStore) LoadHistory(matchID string) ([]byte, error) {
	ctx := context.Background()
	return s.client.Get(ctx, s.historyKey(matchID)).Bytes()
}

func (s *RedisMatchStore) SaveEvents(matchID string, events []byte) error {
	ctx := context.Background()
	return s.client.Set(ctx, s.eventsKey(matchID), events, matchTTL).Err()
}

func (s *RedisMatchStore) LoadEvents(matchID string) ([]byte, error) {
	ctx := context.Background()
	return s.client.Get(ctx, s.eventsKey(matchID)).Bytes()
}

func (s *RedisMatchStore) SavePresence(matchID string, presence []byte) error {
	ctx := context.Background()
	return s.client.Set(ctx, s.presenceKey(matchID), presence, presenceTTL).Err()
}

// SaveSnapshotAtomic persists every per-mutation component of a match
// snapshot (full state, hashed seat secrets, history, events, presence,
// seen client move IDs) as ONE pipelined round trip.
//
// Why it exists: the service's saveToRedis used to issue these writes as
// six sequential Save* calls. Against a same-host Redis that is invisible,
// but the hosted deployment talks to Upstash across the WAN (~70-90ms per
// round trip), which put a hard ~0.5s network floor on every player move
// (and again on the computer opponent's reply, doubling the perceived
// input-to-response delay). A pipeline sends all writes in one round trip;
// correctness is unchanged -- the keys are independent, execution order
// within a pipeline is preserved, and per-command errors still surface
// through Exec. The individual Save* methods remain for the narrower
// callers (create-race Flush paths, token stores, tests).
func (s *RedisMatchStore) SaveSnapshotAtomic(matchID string, state []byte, secretWhite, secretBlack string, history, events, presence, seenIDs []byte) error {
	secrets, err := json.Marshal(map[string]string{"white": secretWhite, "black": secretBlack})
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	ctx := context.Background()
	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.stateKey(matchID), state, matchTTL)
	pipe.Set(ctx, s.secretsKey(matchID), secrets, matchTTL)
	if len(history) > 0 {
		pipe.Set(ctx, s.historyKey(matchID), history, matchTTL)
	}
	if len(events) > 0 {
		pipe.Set(ctx, s.eventsKey(matchID), events, matchTTL)
	}
	if len(presence) > 0 {
		pipe.Set(ctx, s.presenceKey(matchID), presence, presenceTTL)
	}
	if len(seenIDs) > 0 {
		pipe.Set(ctx, s.seenIDsKey(matchID), seenIDs, matchTTL)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisMatchStore) LoadPresence(matchID string) ([]byte, error) {
	ctx := context.Background()
	return s.client.Get(ctx, s.presenceKey(matchID)).Bytes()
}

// seqTTL is a backstop for the sequence counter, not a liveness bound: it is
// deliberately longer than any other key's TTL (matchTTL is 1h). A short TTL
// here would let the counter reset while match state still exists, and since
// clients drop snapshots whose seq is lower than one they have already seen,
// a reset counter would freeze every open board. Seven days only stops
// counters for matches whose container was never evicted (crashed process,
// abandoned Redis) from accumulating forever.
const seqTTL = 7 * 24 * time.Hour

// IncSeq increments the match's shared sequence counter and refreshes its TTL
// in one pipelined round trip.
func (s *RedisMatchStore) IncSeq(matchID string) (int64, error) {
	ctx := context.Background()
	key := s.seqKey(matchID)
	pipe := s.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, seqTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (s *RedisMatchStore) LoadSeq(matchID string) (int64, error) {
	ctx := context.Background()
	val, err := s.client.Get(ctx, s.seqKey(matchID)).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return strconv.ParseInt(val, 10, 64)
}

// LoadHydrationBundle fetches the four keys a container rebuild needs in one
// pipelined round trip. A missing key is returned as nil / zero rather than
// an error; only a transport-level failure fails the call.
func (s *RedisMatchStore) LoadHydrationBundle(matchID string) (state, presence, seenIDs []byte, seq int64, err error) {
	ctx := context.Background()
	pipe := s.client.Pipeline()
	stateCmd := pipe.Get(ctx, s.stateKey(matchID))
	presenceCmd := pipe.Get(ctx, s.presenceKey(matchID))
	seenCmd := pipe.Get(ctx, s.seenIDsKey(matchID))
	seqCmd := pipe.Get(ctx, s.seqKey(matchID))
	// Exec surfaces the first command's error; a missing key arrives as
	// redis.Nil and is normal here, so only other errors are failures.
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, nil, nil, 0, err
	}
	state = bytesOrNil(stateCmd)
	presence = bytesOrNil(presenceCmd)
	seenIDs = bytesOrNil(seenCmd)
	if raw, err := seqCmd.Result(); err == nil {
		if parsed, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
			seq = parsed
		}
	}
	return state, presence, seenIDs, seq, nil
}

// bytesOrNil treats a missing key (redis.Nil) as absent data, not an error.
func bytesOrNil(cmd *redis.StringCmd) []byte {
	data, err := cmd.Bytes()
	if err != nil {
		return nil
	}
	return data
}

func (s *RedisMatchStore) SaveSeenClientMoveIDs(matchID string, ids []byte) error {
	ctx := context.Background()
	return s.client.Set(ctx, s.seenIDsKey(matchID), ids, matchTTL).Err()
}

func (s *RedisMatchStore) LoadSeenClientMoveIDs(matchID string) ([]byte, error) {
	ctx := context.Background()
	return s.client.Get(ctx, s.seenIDsKey(matchID)).Bytes()
}

func (s *RedisMatchStore) DeleteMatch(matchID string) error {
	ctx := context.Background()
	pipe := s.client.Pipeline()
	pipe.Del(ctx, s.stateKey(matchID))
	pipe.Del(ctx, s.secretsKey(matchID))
	pipe.Del(ctx, s.historyKey(matchID))
	pipe.Del(ctx, s.eventsKey(matchID))
	pipe.Del(ctx, s.presenceKey(matchID))
	pipe.Del(ctx, s.seqKey(matchID))
	pipe.Del(ctx, s.seenIDsKey(matchID))
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisMatchStore) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.client.Ping(ctx).Err()
}

func (s *RedisMatchStore) Close() error {
	return s.client.Close()
}
