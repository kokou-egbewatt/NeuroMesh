// Package logging builds the slog logger every NeuroMesh service uses: JSON
// to stdout, one record per line, level from config. Services never write log
// files and never ship logs themselves; on the cluster the collector agent
// reads stdout.
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

// New returns a JSON logger at level, tagged with the service name.
func New(w io.Writer, service, level string) (*slog.Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(h).With("service", service), nil
}
