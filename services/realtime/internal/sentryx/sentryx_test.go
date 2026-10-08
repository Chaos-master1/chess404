package sentryx

import (
	"errors"
	"testing"
	"time"
)

func TestInitWithoutDSNIsDisabled(t *testing.T) {
	Enabled = false
	t.Setenv("SENTRY_DSN", "")
	Init(Config{Service: "test-service"})
	if Enabled {
		t.Fatal("Init must not enable Sentry when SENTRY_DSN is empty")
	}
}

func TestInitReadsDSNFromEnv(t *testing.T) {
	Enabled = false
	t.Setenv("SENTRY_DSN", "https://examplekey@o0.ingest.de.sentry.io/0")
	Init(Config{Service: "test-service", Environment: "test", Release: "test"})
	if !Enabled {
		t.Fatal("Init must enable Sentry from the SENTRY_DSN environment variable")
	}
	Flush(time.Second)
	Enabled = false
}

func TestCaptureErrorDisabledIsNoop(t *testing.T) {
	Enabled = false
	// Must not panic and must not block.
	CaptureError(errors.New("boom"), map[string]string{"k": "v"})
}

func TestFlushDisabledReturnsTrue(t *testing.T) {
	Enabled = false
	if !Flush(time.Second) {
		t.Fatal("Flush without an enabled client must report success (true)")
	}
}

func TestSanitizeStripsNewlines(t *testing.T) {
	got := sanitize("bad\ndsn\r\nhere")
	if got != "baddsnhere" {
		t.Fatalf("sanitize = %q, want %q", got, "baddsnhere")
	}
}

func TestInitWithInvalidDSNDoesNotEnable(t *testing.T) {
	Enabled = false
	Init(Config{Service: "test-service", DSN: "not-a-dsn", Environment: "test"})
	if Enabled {
		t.Fatal("Init with an invalid DSN must disable Sentry, not boot-fail")
	}
	Flush(time.Second)
}
