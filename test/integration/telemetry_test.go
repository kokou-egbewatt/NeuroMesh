package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/kokou-egbewatt/NeuroMesh/packages/tracing"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

// recordSpans installs a TracerProvider that keeps every span in memory, for the duration of t.
// Both services run in this process, so one provider sees the whole trace.
func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp, err := tracing.NewProvider("integration", "test", 1, sdktrace.WithSyncer(exp))
	if err != nil {
		t.Fatal(err)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(t.Context())
		otel.SetTracerProvider(noop.NewTracerProvider())
	})
	return exp
}

func findSpan(spans tracetest.SpanStubs, name string, kind trace.SpanKind) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name && spans[i].SpanKind == kind {
			return &spans[i]
		}
	}
	return nil
}

// One request is one trace: the gateway's HTTP span, its client span to the runtime, and the
// runtime's server span as that client span's child, propagated in the gRPC metadata (#10).
func TestOneRequestIsOneTraceAcrossGatewayAndRuntime(t *testing.T) {
	exp := recordSpans(t)
	s := harness.Boot(t)
	r := post(t, s.HTTPSClient(10*time.Second), s.GatewayURL+"/v1/route", "trace-1", `{"model":"llama3","prompt":"hello there world"}`)
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}

	spans := exp.GetSpans()
	edge := findSpan(spans, "POST /v1/route", trace.SpanKindServer)
	call := findSpan(spans, "neuromesh.v1.InferenceService/Route", trace.SpanKindClient)
	served := findSpan(spans, "neuromesh.v1.InferenceService/Route", trace.SpanKindServer)
	if edge == nil || call == nil || served == nil {
		names := make([]string, 0, len(spans))
		for _, sp := range spans {
			names = append(names, sp.Name+"/"+sp.SpanKind.String())
		}
		t.Fatalf("missing spans; got %v", names)
	}
	traceID := edge.SpanContext.TraceID()
	for _, sp := range []*tracetest.SpanStub{call, served} {
		if sp.SpanContext.TraceID() != traceID {
			t.Errorf("%s/%s is in trace %s, want %s", sp.Name, sp.SpanKind, sp.SpanContext.TraceID(), traceID)
		}
	}
	if call.Parent.SpanID() != edge.SpanContext.SpanID() {
		t.Error("the client span to the runtime is not a child of the gateway's HTTP span")
	}
	if served.Parent.SpanID() != call.SpanContext.SpanID() {
		t.Error("the runtime's server span is not a child of the gateway's client span")
	}
	if findSpan(spans, "GET /healthz", trace.SpanKindServer) != nil {
		t.Error("health checks are traced")
	}
}

// Nothing from a request's content reaches a span attribute or a metric label: not the prompt, not
// the request ID (#10). The prompt is a sentinel that appears nowhere else.
func TestNoHighCardinalityValuesInSpansOrMetrics(t *testing.T) {
	exp := recordSpans(t)
	s := harness.BootWith(t, harness.Options{Metrics: true})
	const sentinel = "SENTINEL-7f3a9c-prompt-must-not-leak"
	const rid = "leak-check-5b2e"
	r := post(t, s.HTTPSClient(10*time.Second), s.GatewayURL+"/v1/route", rid, `{"model":"llama3","prompt":"`+sentinel+`"}`)
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}

	for _, sp := range exp.GetSpans() {
		for _, kv := range append(sp.Attributes, sp.Resource.Attributes()...) {
			v := kv.Value.String()
			if strings.Contains(v, sentinel) || strings.Contains(v, rid) {
				t.Errorf("span %s carries %s=%q", sp.Name, kv.Key, v)
			}
		}
		for _, ev := range sp.Events {
			for _, kv := range ev.Attributes {
				if v := kv.Value.String(); strings.Contains(v, sentinel) || strings.Contains(v, rid) {
					t.Errorf("span %s event %s carries %s=%q", sp.Name, ev.Name, kv.Key, v)
				}
			}
		}
	}
	for svc, m := range s.Metrics {
		mfs, err := m.Gatherer().Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, mf := range mfs {
			for _, metric := range mf.GetMetric() {
				for _, l := range metric.GetLabel() {
					if strings.Contains(l.GetValue(), sentinel) || strings.Contains(l.GetValue(), rid) {
						t.Errorf("%s metric %s has label %s=%q", svc, mf.GetName(), l.GetName(), l.GetValue())
					}
				}
			}
		}
	}
}
