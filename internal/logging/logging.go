// Package logging builds the structured logger used across orrery.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLevel converts a human-written level name into an slog.Level.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", name)
	}
}

// New builds a logger writing to w. format is "text" or "json".
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lvl}

	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		h = slog.NewJSONHandler(w, opts)
	case "text", "":
		h = slog.NewTextHandler(w, opts)
	default:
		return nil, fmt.Errorf("unknown log format %q", format)
	}
	return slog.New(h), nil
}

// Discard returns a logger that throws everything away. Useful in tests that
// exercise components without asserting on their log output.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
