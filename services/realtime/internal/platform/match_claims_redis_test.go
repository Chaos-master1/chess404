package platform

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisMatchClaimStoreRoundTripsClaims(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("expected miniredis to start, got %v", err)
	}
	defer server.Close()

	store, err := NewRedisMatchClaimStore("redis://"+server.Addr()+"/0", "claims:test")
	if err != nil {
		t.Fatalf("expected redis claim store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	claim := MatchSeatClaim{
		MatchID:      "room_live",
		GuestID:      "guest_alpha",
		SeatColor:    "white",
		PlayerID:     "guest_alpha",
		PlayerSecret: "seat_secret_alpha",
		Queue:        "rated",
	}
	if err := store.Put(claim); err != nil {
		t.Fatalf("expected claim put to succeed, got %v", err)
	}

	loaded, ok := store.Get("room_live", "guest_alpha")
	if !ok {
		t.Fatalf("expected claim lookup to succeed")
	}
	if loaded.PlayerSecret != claim.PlayerSecret || loaded.SeatColor != "white" || loaded.ClaimToken == "" {
		t.Fatalf("expected persisted claim to round-trip, got %#v", loaded)
	}
	if loaded.ExpiresAt.IsZero() {
		t.Fatalf("expected persisted claim to have an expiry, got %#v", loaded)
	}

	tokenClaim, ok := store.GetByToken("room_live", loaded.ClaimToken)
	if !ok || tokenClaim.GuestID != "guest_alpha" {
		t.Fatalf("expected token lookup to succeed, got %#v %#v", ok, tokenClaim)
	}
	if _, ok := store.Get("room_live", "guest_alpha"); ok {
		t.Fatalf("expected token lookup to consume the stored claim")
	}

	reloaded, err := NewRedisMatchClaimStore("redis://"+server.Addr()+"/0", "claims:test")
	if err != nil {
		t.Fatalf("expected redis claim store reload to succeed, got %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	if _, ok := reloaded.Get("room_live", "guest_alpha"); ok {
		t.Fatalf("expected consumed claim to stay deleted after reload")
	}
	if reloaded.Stats().CachedClaims != 0 {
		t.Fatalf("expected cached claim stats to reflect stored claim, got %#v", reloaded.Stats())
	}
}

// TestRedisMatchClaimStoreDegradesGracefullyWhenRedisIsUnreachable exercises
// the actual failure path for L4 of the pre-launch review (Upstash headroom
// is a real, previously-hit constraint) -- not by reading the code and
// inferring behavior, but by starting a real (if in-process) Redis server,
// letting the store initialize against it, then killing it mid-flight and
// observing what Put() actually does, the same way a live Upstash outage or
// rate-limit rejection would present.
//
// Confirms an asymmetry worth knowing before an incident: Put() updates the
// in-memory map UNCONDITIONALLY before attempting the Redis write
// (match_claims.go's Put, line ~201-202), so a claim already exists on THIS
// instance immediately regardless of Redis's health -- but Put() still
// returns the Redis error to the caller. Two call-site patterns exist in
// cmd/platform-service/main.go: claim RENEWAL paths ignore this error
// (`if err := claims.Put(claim); err == nil { ...refresh... }`, e.g. line
// 456) and fall through to returning the already-updated claim anyway --
// but NEW claim issuance paths treat it as a hard failure
// (`if err != nil { http.Error(w, ..., 500); return }`, e.g. lines 477,
// 529). So a Redis outage's actual blast radius is narrower than "claims
// break": existing/renewing claims keep working, but creating a NEW
// match/room or joining a seat for the first time would fail with a 500
// until Redis recovers.
func TestRedisMatchClaimStoreDegradesGracefullyWhenRedisIsUnreachable(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("expected miniredis to start, got %v", err)
	}

	store, err := NewRedisMatchClaimStore("redis://"+server.Addr()+"/0", "claims:test")
	if err != nil {
		t.Fatalf("expected redis claim store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	// Simulate an Upstash outage / cap-hit mid-match: the server this store
	// already successfully connected to goes away.
	server.Close()

	claim := MatchSeatClaim{
		MatchID:      "room_during_outage",
		GuestID:      "guest_beta",
		SeatColor:    "black",
		PlayerID:     "guest_beta",
		PlayerSecret: "seat_secret_beta",
		Queue:        "rated",
	}
	putErr := store.Put(claim)
	if putErr == nil {
		t.Fatal("expected Put to report the Redis write failure once the server is gone, got nil error -- if this now succeeds, either miniredis changed behavior or Put started swallowing errors silently, both worth knowing about")
	}
	t.Logf("Put correctly reported the outage: %v", putErr)

	// The important part: the claim must still be usable from THIS
	// instance's in-memory cache despite the persistence failure -- this is
	// what makes the "ignore the Put error and use the claim anyway" call
	// sites in cmd/platform-service/main.go (claim renewal) safe.
	loaded, ok := store.Get("room_during_outage", "guest_beta")
	if !ok {
		t.Fatal("CONFIRMED GAP: Put's in-memory update did not survive a Redis persistence failure -- claim renewal call sites that ignore the Put error would silently serve a claim that was never actually written")
	}
	if loaded.PlayerSecret != claim.PlayerSecret {
		t.Fatalf("expected the in-memory claim to match what was put, got %#v", loaded)
	}
}

func TestMatchClaimStoreExpiresClaims(t *testing.T) {
	store := NewMatchClaimStoreWithTTL(20 * time.Millisecond)

	if err := store.Put(MatchSeatClaim{
		MatchID:      "room_expire",
		GuestID:      "guest_expire",
		SeatColor:    "white",
		PlayerID:     "guest_expire",
		PlayerSecret: "expire_secret",
	}); err != nil {
		t.Fatalf("expected claim put to succeed, got %v", err)
	}

	time.Sleep(35 * time.Millisecond)

	if _, ok := store.Get("room_expire", "guest_expire"); ok {
		t.Fatalf("expected expired claim to be evicted")
	}
	if store.Stats().CachedClaims != 0 {
		t.Fatalf("expected expired claim stats to be empty, got %#v", store.Stats())
	}
}
