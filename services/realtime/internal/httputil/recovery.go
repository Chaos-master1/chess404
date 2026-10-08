package httputil

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/chess404/realtime/internal/sentryx"
)

func WithRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Report to Sentry (no-op when disabled) with request context
				// as tags so panics are triageable per route.
				sentryx.CaptureError(fmt.Errorf("panic: %v", err), map[string]string{
					"method": r.Method,
					"path":   r.URL.Path,
				})
				slog.Error("panic recovered",
					"error", err,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				WriteError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
