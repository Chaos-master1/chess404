package logging

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/sentryx"
)

type Logger struct {
	slogger *slog.Logger
}

func New(service string) *Logger {
	level := parseLevel()
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slogger := slog.New(handler).With("service", service)
	return &Logger{slogger: slogger}
}

// sentryTags extracts string key/value pairs from slog fields so log
// context (e.g. "match_id") becomes Sentry tags.
func (l *Logger) sentryTags(fields []any) map[string]string {
	tags := map[string]string{}
	for i := 0; i+1 < len(fields); i += 2 {
		if k, ok := fields[i].(string); ok {
			switch v := fields[i+1].(type) {
			case string:
				tags[k] = v
			case fmt.Stringer:
				tags[k] = v.String()
			}
		}
	}
	return tags
}

func parseLevel() slog.Level {
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (l *Logger) Debug(msg string, fields ...any) {
	l.slogger.Debug(msg, fields...)
}

func (l *Logger) Info(msg string, fields ...any) {
	l.slogger.Info(msg, fields...)
}

func (l *Logger) Warn(msg string, fields ...any) {
	l.slogger.Warn(msg, fields...)
}

func (l *Logger) Error(msg string, fields ...any) {
	l.slogger.Error(msg, fields...)
	// Mirror error-level logs to Sentry so backend failures surface in the
	// chess404 org. No-op unless sentryx.Init ran with a DSN.
	sentryx.CaptureError(errors.New(msg), l.sentryTags(fields))
}

func (l *Logger) Fatal(msg string, fields ...any) {
	l.slogger.Error(msg, fields...)
	sentryx.CaptureError(errors.New(msg), l.sentryTags(fields))
	// Give queued Sentry events a moment to reach the server before the
	// process dies, otherwise shutdown reports are silently dropped.
	sentryx.Flush(2 * time.Second)
	os.Exit(1)
}
