// Package config loads YAML config files with environment variable overrides,
// and turns the TLS section every service shares into a crypto/tls config.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads the YAML file at path into dst. found reports whether the file
// existed: a missing file is not an error, so defaults and environment
// variables still apply, but callers must log it, because a mistyped
// CONFIG_PATH otherwise runs on defaults without a word.
//
// Unknown keys are an error. A key the service does not read is either a typo
// or a setting that silently does nothing, and both are worth failing on.
func Load(path string, dst any) (found bool, err error) {
	f, err := os.Open(path) //nolint:gosec // the path is operator-supplied config, not request input
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = f.Close() }()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		return true, fmt.Errorf("parse %s: %w", path, err)
	}
	return true, nil
}

// StringEnv returns the environment variable named key, or fallback if unset or empty.
func StringEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
