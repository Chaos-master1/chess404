// Package sentryx wires Sentry error reporting into the Go backend services.
//
// Init is gated on SENTRY_DSN: unset means Sentry is disabled and the
// service runs exactly as before (a single boot log line records that).
// This keeps local development, tests, and staging without a DSN free of
// network calls and sentinel behavior changes.
package sentryx

import (
	"fmt"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config controls how sentryx.Init initializes the SDK.
type Config struct {
	// DSN from SENTRY_DSN. Empty disables Sentry entirely.
	DSN string
	// Service name, reported as the `service` tag on every event.
	Service string
	// Environment from SENTRY_ENVIRONMENT (default "development").
	Environment string
	// Release, e.g. a git SHA (default SENTRY_RELEASE or "dev").
	Release string
}

// Enabled reports whether Init successfully enabled Sentry. False before
// Init runs.
var Enabled bool

// Init initializes Sentry from the environment. It never fails the boot:
// a broken DSN logs a warning and disables reporting, matching the
// codebase convention that observability must not be boot-fatal.
func Init(cfg Config) {
	if cfg.DSN == "" {
		cfg.DSN = os.Getenv("SENTRY_DSN")
	}
	if cfg.DSN == "" {
		fmt.Println(`{"level":"INFO","msg":"sentry disabled: SENTRY_DSN not set"}`)
		return
	}
	if cfg.Environment == "" {
		cfg.Environment = os.Getenv("SENTRY_ENVIRONMENT")
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.Release == "" {
		cfg.Release = os.Getenv("SENTRY_RELEASE")
	}
	if cfg.Release == "" {
		cfg.Release = "dev"
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		Release:     cfg.Release,
		// Local variables are not shipped, keep stack traces lean.
		AttachStacktrace: true,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if event == nil {
				return nil
			}
			if event.Tags == nil {
				event.Tags = map[string]string{}
			}
			event.Tags["service"] = cfg.Service
			return event
		},
	})
	if err != nil {
		fmt.Printf(`{"level":"WARN","msg":"sentry disabled: invalid SENTRY_DSN: %s"}`+"\n", sanitize(err.Error()))
		return
	}
	Enabled = true
}

// CaptureError reports err to Sentry with optional context tags and a
// finished span, if any, from ctx. No-op when Sentry is disabled.
func CaptureError(err error, tags map[string]string) string {
	if !Enabled || err == nil {
		return ""
	}
	hub := sentry.CurrentHub().Clone()
	scope := hub.Scope()
	for k, v := range tags {
		scope.SetTag(k, v)
	}
	hub.CaptureException(err)
	return ""
}

// Flush waits up to timeout for queued events to reach Sentry. Call before
// process exit (see logging.Fatal) so shutdown errors are not lost.
func Flush(timeout time.Duration) bool {
	if !Enabled {
		return true
	}
	return sentry.Flush(timeout)
}

// sanitize strips newlines from error text so boot logs stay one-line JSON.
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' && s[i] != '\r' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
