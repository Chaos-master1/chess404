package httputil

import (
	"testing"
	"time"
)

// The circuit breaker protects outbound service-to-service calls. Its state
// machine is availability-critical: it must trip open at the failure
// threshold, refuse calls while open, probe with limited traffic after the
// cooldown, close again only after sustained success, and reopen on the first
// half-open failure. The timeout is shortened via in-package access so tests
// stay fast; the production default (30s) is asserted explicitly.
func TestBreakerDefaultsAndPoolIdentity(t *testing.T) {
	pool := NewBreakerPool()
	cb := pool.Get("match-service")
	if cb.State() != "closed" {
		t.Fatalf("fresh breaker must be closed, got %q", cb.State())
	}
	if cb.failureThresh != 5 || cb.successThresh != 3 || cb.halfOpenMax != 3 {
		t.Fatalf("unexpected breaker tuning: failureThresh=%d successThresh=%d halfOpenMax=%d",
			cb.failureThresh, cb.successThresh, cb.halfOpenMax)
	}
	if cb.timeout != 30*time.Second {
		t.Fatalf("production cooldown must stay 30s, got %v", cb.timeout)
	}
	if again := pool.Get("match-service"); again != cb {
		t.Fatalf("Get must return the same breaker for the same name")
	}
	if other := pool.Get("platform-service"); other == cb {
		t.Fatalf("Get must return distinct breakers for distinct names")
	}
}

func TestBreakerOpensAfterFailureThresholdAndBlocks(t *testing.T) {
	cb := NewBreakerPool().Get("db")
	for i := 0; i < cb.failureThresh-1; i++ {
		if !cb.Allow() {
			t.Fatalf("closed breaker must allow calls (failure %d)", i+1)
		}
		cb.RecordFailure()
	}
	if cb.State() != "closed" {
		t.Fatalf("breaker below threshold must stay closed, got %q", cb.State())
	}

	if !cb.Allow() {
		t.Fatalf("closed breaker must allow the threshold-crossing call")
	}
	cb.RecordFailure() // hits the threshold -> opens
	if cb.State() != "open" {
		t.Fatalf("breaker at failure threshold must open, got %q", cb.State())
	}
	if cb.Allow() {
		t.Fatalf("open breaker must refuse calls during cooldown")
	}
}

func TestBreakerClosedSuccessResetsFailureCount(t *testing.T) {
	cb := NewBreakerPool().Get("db")
	for i := 0; i < cb.failureThresh-1; i++ {
		cb.RecordFailure()
	}
	cb.RecordSuccess() // resets the consecutive-failure counter
	if cb.State() != "closed" {
		t.Fatalf("success must keep the breaker closed, got %q", cb.State())
	}
	for i := 0; i < cb.failureThresh-1; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "closed" {
		t.Fatalf("failures after a success restart the count; %d more must stay closed, got %q",
			cb.failureThresh-1, cb.State())
	}
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("the next consecutive failure must open the breaker, got %q", cb.State())
	}
}

func TestBreakerHalfOpenAfterCooldownThenClosesOnSustainedSuccess(t *testing.T) {
	cb := NewBreakerPool().Get("db")
	cb.timeout = 30 * time.Millisecond // test-only cooldown
	for i := 0; i < cb.failureThresh; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "open" {
		t.Fatalf("setup: breaker must be open, got %q", cb.State())
	}

	time.Sleep(40 * time.Millisecond) // past the shortened cooldown
	if !cb.Allow() {
		t.Fatalf("after the cooldown the breaker must admit a probe")
	}
	if cb.State() != "half-open" {
		t.Fatalf("first allow after cooldown must move to half-open, got %q", cb.State())
	}

	// Probes are rate-limited to halfOpenMax concurrent calls.
	for i := 0; i < cb.halfOpenMax; i++ {
		if !cb.Allow() {
			t.Fatalf("half-open must admit up to %d probes, refused at %d", cb.halfOpenMax, i+1)
		}
	}
	if cb.Allow() {
		t.Fatalf("half-open must refuse probes beyond halfOpenMax=%d", cb.halfOpenMax)
	}

	for i := 0; i < cb.successThresh; i++ {
		cb.RecordSuccess()
	}
	if cb.State() != "closed" {
		t.Fatalf("%d half-open successes must close the breaker, got %q", cb.successThresh, cb.State())
	}
	if !cb.Allow() {
		t.Fatalf("closed breaker must allow calls again")
	}

	// The failure counter must have been reset: a fresh streak is required to
	// re-open, not a single failure.
	cb.RecordFailure()
	if cb.State() != "closed" {
		t.Fatalf("one failure after closing must not re-open, got %q", cb.State())
	}
}

func TestBreakerHalfOpenFailureReopensImmediately(t *testing.T) {
	cb := NewBreakerPool().Get("db")
	cb.timeout = 30 * time.Millisecond
	for i := 0; i < cb.failureThresh; i++ {
		cb.RecordFailure()
	}
	time.Sleep(40 * time.Millisecond)

	if !cb.Allow() {
		t.Fatalf("setup: expected a half-open probe")
	}
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("a failed half-open probe must re-open the breaker, got %q", cb.State())
	}
	if cb.Allow() {
		t.Fatalf("re-opened breaker must refuse calls until the next cooldown")
	}
}
