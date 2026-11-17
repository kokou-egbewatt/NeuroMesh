package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceIDsOnlyWithASpan(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, "svc", "info", "json")
	if err != nil {
		t.Fatal(err)
	}
	log.InfoContext(context.Background(), "no span")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	log.InfoContext(trace.ContextWithSpanContext(context.Background(), sc), "with span")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %s", len(lines), buf.String())
	}
	var without, with map[string]any
	_ = json.Unmarshal(lines[0], &without)
	_ = json.Unmarshal(lines[1], &with)
	if _, ok := without["trace_id"]; ok {
		t.Errorf("trace_id present without a span: %s", lines[0])
	}
	if with["trace_id"] != sc.TraceID().String() || with["span_id"] != sc.SpanID().String() {
		t.Errorf("ids missing or wrong with a span: %s", lines[1])
	}
	if with["service"] != "svc" {
		t.Errorf("service lost through the wrapper: %s", lines[1])
	}
}
