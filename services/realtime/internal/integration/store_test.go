//go:build integration

// Package integration holds tests that need real Postgres/Redis, not the
// file/sqlite/in-memory backends the rest of the suite uses. Run via:
//
//	go test ./internal/integration/... -tags=integration
//
// against TEST_POSTGRES_URL / TEST_REDIS_URL (see
// deploy/docker-compose.integration.yml, which provisions both and is what
// .github/workflows/ci.yml's go-integration job runs against).
//
// This package did not exist before: the CI job and the local dev command
// documented in CLAUDE.md both referenced it, but it was never built --
// go-integration has been failing (or silently matching zero packages) on
// every run since the job was added. Deliberately scoped to real,
// meaningful coverage of the postgres/redis-backed stores that had NONE
// (15 of 20 internal/platform/*_postgres.go / *_sqlite.go files had no
// backend-specific test at all), not an exhaustive integration suite --
// the point of this pass is making the CI job trustworthy again, which
// every other fix's verification quality depends on.
package integration

import (
	"os"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/platform"
	"github.com/chess404/realtime/internal/rate_limit"
)

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s not set -- run via docker compose -f deploy/docker-compose.integration.yml (see package doc)", key)
	}
	return v
}

// TestPostgresAccountStoreRoundTrip proves the real Postgres-backed account
// store persists and reads back correctly against an actual Postgres
// server -- the file/sqlite variants already have their own tests, but
// nothing previously exercised this against real Postgres wire behavior
// (connection pooling, actual SQL types, real transaction semantics).
func TestPostgresAccountStoreRoundTrip(t *testing.T) {
	dsn := requireEnv(t, "TEST_POSTGRES_URL")

	store, err := platform.NewPostgresAccountStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresAccountStore: %v", err)
	}
	defer store.Close()

	if store.Backend() != "postgres" {
		t.Fatalf("expected backend %q, got %q", "postgres", store.Backend())
	}

	now := time.Now().UTC()
	suffix := now.Format("20060102150405.000000000")
	guest := platform.GuestProfile{
		GuestID:     "integration_guest_" + suffix,
		DisplayName: "Integration Test Guest",
		CreatedAt:   now,
		LastSeenAt:  now,
	}
	handle := "integration_test_" + suffix
	session, err := store.RegisterGuestAccount(guest, handle, "integration-test@example.com", "not-a-real-password-123")
	if err != nil {
		t.Fatalf("RegisterGuestAccount: %v", err)
	}
	if session.Account.AccountID == "" {
		t.Fatal("expected a non-empty AccountID from RegisterGuestAccount")
	}

	fetched, ok := store.GetAccount(session.Account.AccountID)
	if !ok {
		t.Fatalf("expected account %s to round-trip through real Postgres, got not-found", session.Account.AccountID)
	}
	if fetched.Handle != session.Account.Handle {
		t.Fatalf("expected handle %q to round-trip, got %q", session.Account.Handle, fetched.Handle)
	}
}

// TestRedisRateLimiterEnforcesRealLimit proves the Redis-backed rate limiter
// actually enforces its limit against a real Redis server -- the in-memory
// fallback already has unit coverage, but the Lua-script token-bucket path
// (internal/rate_limit/rate_limit.go's RedisRateLimiter) had never been
// exercised against a real Redis instance.
func TestRedisRateLimiterEnforcesRealLimit(t *testing.T) {
	redisURL := requireEnv(t, "TEST_REDIS_URL")

	limiter, err := rate_limit.NewRedis(redisURL)
	if err != nil {
		t.Fatalf("rate_limit.NewRedis: %v", err)
	}

	key := "integration_test_ratelimit_" + time.Now().UTC().Format("20060102150405.000000000")
	const limit = 3
	for i := 0; i < limit; i++ {
		allowed, _ := limiter.Allow(key, time.Minute, limit)
		if !allowed {
			t.Fatalf("expected request %d/%d to be allowed, got denied", i+1, limit)
		}
	}
	if allowed, retryAfter := limiter.Allow(key, time.Minute, limit); allowed {
		t.Fatalf("expected request %d to be denied once the limit is exhausted, got allowed (retryAfter=%v)", limit+1, retryAfter)
	}
}
