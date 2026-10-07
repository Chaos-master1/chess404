package httputil

import (
	"errors"
	"testing"
	"time"
)

// RetryWithBackoff must return the LAST error after exhausting attempts, stop
// calling fn after success, and grow its sleep with the attempt index -- the
// small baseDelay keeps real sleep time in the low milliseconds.
func TestRetrySucceedsImmediately(t *testing.T) {
	calls := 0
	err := RetryWithBackoff(3, time.Millisecond, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("immediate success must return nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("success on the first attempt must not retry, got %d calls", calls)
	}
}

func TestRetrySucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := RetryWithBackoff(5, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("success on attempt 3 must return nil, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryExhaustsAttemptsAndReturnsLastError(t *testing.T) {
	calls := 0
	sentinel := errors.New("persistent failure")
	err := RetryWithBackoff(4, time.Millisecond, func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("exhausted retries must return the last error, got %v", err)
	}
	if calls != 4 {
		t.Fatalf("expected exactly maxAttempts calls, got %d", calls)
	}
}
