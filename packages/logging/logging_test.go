package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{"": slog.LevelInfo, "info": slog.LevelInfo, "DEBUG": slog.LevelDebug, "warn": slog.LevelWarn, "error": slog.LevelError}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("an unknown level must fail")
	}
}

func TestNewWritesOneJSONRecordPerLine(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, "gateway", "info", "")
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("hidden")
	log.Info("request served", "prompt", "line one\n\"quoted\"")
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (debug suppressed, newline in a field escaped)", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if rec["service"] != "gateway" || rec["msg"] != "request served" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestTextFormatAndUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, "runtime", "info", "text")
	if err != nil {
		t.Fatal(err)
	}
	log.Info("ready", "addr", ":50051")
	if got := buf.String(); !bytes.Contains([]byte(got), []byte("msg=ready")) || !bytes.Contains([]byte(got), []byte("service=runtime")) {
		t.Fatalf("text output: %q", got)
	}
	if _, err := New(&buf, "x", "info", "xml"); err == nil {
		t.Fatal("an unknown format must fail")
	}
}
