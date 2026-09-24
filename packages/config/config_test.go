package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type sample struct {
	Addr    string        `yaml:"addr"`
	Timeout time.Duration `yaml:"timeout"`
	TLS     TLS           `yaml:"tls"`
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	var s sample
	found, err := Load(filepath.Join(t.TempDir(), "absent.yaml"), &s)
	if err != nil || found {
		t.Fatalf("found=%v err=%v, want false, nil", found, err)
	}
}

func TestLoadParsesDurationsAndNestedTLS(t *testing.T) {
	var s sample
	found, err := Load(write(t, "addr: \":1\"\ntimeout: 5s\ntls:\n  ca_file: ca.crt\n  insecure: true\n"), &s)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if s.Addr != ":1" || s.Timeout != 5*time.Second || s.TLS.CAFile != "ca.crt" || !s.TLS.Insecure {
		t.Fatalf("unexpected result: %+v", s)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	var s sample
	if _, err := Load(write(t, "addr: \":1\"\nlog_lvl: debug\n"), &s); err == nil {
		t.Fatal("a misspelled key must fail, not be ignored")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	var s sample
	found, err := Load(write(t, ""), &s)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestStringEnv(t *testing.T) {
	t.Setenv("NM_TEST_ENV", "")
	if got := StringEnv("NM_TEST_ENV", "fb"); got != "fb" {
		t.Fatalf("empty env: got %q", got)
	}
	t.Setenv("NM_TEST_ENV", "set")
	if got := StringEnv("NM_TEST_ENV", "fb"); got != "set" {
		t.Fatalf("set env: got %q", got)
	}
}
