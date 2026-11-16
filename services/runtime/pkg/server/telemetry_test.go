package server

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/telemetry"
	"github.com/kokou-egbewatt/NeuroMesh/packages/tracing"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

// counts reads one metric family's samples, keyed by their label values joined with "/".
func counts(t *testing.T, m *telemetry.Metrics, name string) map[string]float64 {
	t.Helper()
	mfs, err := m.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			key := ""
			for _, l := range metric.GetLabel() {
				key += l.GetValue() + "/"
			}
			out[key] = sampleValue(mf.GetType(), metric)
		}
	}
	return out
}

func sampleValue(t dto.MetricType, m *dto.Metric) float64 {
	switch t {
	case dto.MetricType_COUNTER:
		return m.GetCounter().GetValue()
	case dto.MetricType_HISTOGRAM:
		return float64(m.GetHistogram().GetSampleCount())
	default:
		return 0
	}
}

// Ten successes and five injected failures through the interceptor are counted exactly, by service,
// rpc and code (#10).
func TestMetricsCountSuccessesAndFailuresExactly(t *testing.T) {
	m := telemetry.New("runtime")
	ic := unaryLogger(quiet, m)
	info := &grpc.UnaryServerInfo{FullMethod: "/neuromesh.v1.InferenceService/Route"}
	ok := func(context.Context, any) (any, error) { return &neuromeshv1.RouteResponse{}, nil }
	fail := func(context.Context, any) (any, error) { return nil, status.Error(codes.Unavailable, "injected") }
	for range 10 {
		_, _ = ic(context.Background(), &neuromeshv1.RouteRequest{}, info, ok)
	}
	for range 5 {
		_, _ = ic(context.Background(), &neuromeshv1.RouteRequest{}, info, fail)
	}

	rpc := info.FullMethod
	want := map[string]float64{"OK/" + rpc + "/runtime/": 10, "Unavailable/" + rpc + "/runtime/": 5}
	if got := counts(t, m, "neuromesh_requests_total"); !equal(got, want) {
		t.Errorf("requests_total = %v, want %v", got, want)
	}
	if got := counts(t, m, "neuromesh_request_errors_total"); !equal(got, map[string]float64{"Unavailable/" + rpc + "/runtime/": 5}) {
		t.Errorf("request_errors_total = %v, want 5 Unavailable", got)
	}
	if got := counts(t, m, "neuromesh_request_duration_seconds"); got[rpc+"/runtime/"] != 15 {
		t.Errorf("duration histogram count = %v, want 15", got)
	}
}

func equal(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range b {
		if a[k] != v {
			return false
		}
	}
	return true
}

// A stream's span carries its chunk count and first-chunk latency, and ends when the stream closes,
// not at its first byte (#10).
func TestStreamSpanCountsChunksAndEndsAtClose(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp, err := tracing.NewProvider("runtime-test", "test", 1, sdktrace.WithSyncer(exp))
	if err != nil {
		t.Fatal(err)
	}
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	addr, _ := start(t, Config{TLS: config.TLS{Insecure: true}})
	conn := dial(t, addr, insecure.NewCredentials())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := neuromeshv1.NewInferenceServiceClient(conn).StreamInference(ctx, &neuromeshv1.StreamRequest{Model: "m", Prompt: "one two three four"})
	if err != nil {
		t.Fatal(err)
	}
	received := 0
	var lastChunk time.Time
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		received++
		lastChunk = time.Now()
	}

	var span *tracetest.SpanStub
	deadline := time.Now().Add(2 * time.Second)
	for span == nil && time.Now().Before(deadline) {
		for _, sp := range exp.GetSpans() {
			if sp.Name == "neuromesh.v1.InferenceService/StreamInference" {
				span = &sp
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if span == nil {
		t.Fatal("no StreamInference span")
	}
	attrs := map[attribute.Key]attribute.Value{}
	for _, kv := range span.Attributes {
		attrs[kv.Key] = kv.Value
	}
	if got := attrs["neuromesh.stream.chunks"].AsInt64(); got != int64(received) || received == 0 {
		t.Errorf("chunks attribute %d, client received %d", got, received)
	}
	if _, ok := attrs["neuromesh.stream.first_chunk_ms"]; !ok {
		t.Error("no first_chunk_ms attribute")
	}
	if span.EndTime.Before(span.StartTime) || span.EndTime.Sub(span.StartTime) < 0 {
		t.Error("span ends before it starts")
	}
	if !lastChunk.IsZero() && span.EndTime.Before(lastChunk.Add(-time.Second)) {
		t.Errorf("span ended at %v, well before the last chunk at %v", span.EndTime, lastChunk)
	}
}
