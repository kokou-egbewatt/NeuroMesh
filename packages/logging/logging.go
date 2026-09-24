// Package logging builds the slog logger every NeuroMesh service uses: one
// record per line on stdout, JSON by default, level from config. Services
// never write log files and never ship logs themselves; on the cluster the
// collector agent reads stdout.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLevel maps a config value to a slog level. Empty means info.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging: unknown level %q (want debug, info, warn or error)", s)
	}
}

// New returns a logger at level, tagged with the service name. format is
// "json" (the default, and the only format outside a laptop: the collector
// parses it) or "text", which local configs use so a terminal stays readable.
func New(w io.Writer, service, level, format string) (*slog.Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		h = slog.NewJSONHandler(w, opts)
	case "text":
		h = slog.NewTextHandler(w, opts)
	default:
		return nil, fmt.Errorf("logging: unknown format %q (want json or text)", format)
	}
	return slog.New(h).With("service", service), nil
}
