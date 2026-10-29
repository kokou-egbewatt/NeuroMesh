package inference

import (
	"context"
	"fmt"
	"testing"

	"github.com/kokou-egbewatt/NeuroMesh/benchmarks/inference/driver"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

// TestSmoke runs every case at a tiny count so CI proves the benchmark still
// boots, connects and measures. Numbers from CI runners are not a baseline.
func TestSmoke(t *testing.T) {
	s := harness.Boot(t)
	for _, c := range Matrix {
		t.Run(fmt.Sprintf("%s/c=%d", c.Scenario, c.Concurrency), func(t *testing.T) {
			row, err := RunCase(context.Background(), s, c, 5, 2*c.Concurrency)
			if err != nil {
				t.Fatal(err)
			}
			if row.Requests != 2*c.Concurrency || row.P50us <= 0 || row.RPS <= 0 {
				t.Fatalf("empty measurement: %+v", row)
			}
			if c.Scenario == "stream" && (row.FirstChunkP50us <= 0 || row.ChunksPerSec <= 0) {
				t.Fatalf("stream row has no first-chunk figures: %+v", row)
			}
		})
	}
}

// BenchmarkRoute and BenchmarkStream expose the matrix to go test -bench: b.N
// requests through the driver at each concurrency, with the percentiles as
// custom metrics. cmd/bench is the recorded baseline; these are for quick
// before and after checks with benchstat.
func BenchmarkRoute(b *testing.B) { benchmarkScenario(b, "route") }

func BenchmarkStream(b *testing.B) { benchmarkScenario(b, "stream") }

func benchmarkScenario(b *testing.B, scenario string) {
	s := harness.Boot(b)
	for _, c := range Matrix {
		if c.Scenario != scenario {
			continue
		}
		b.Run(fmt.Sprintf("c=%d", c.Concurrency), func(b *testing.B) {
			sc, err := NewScenario(s, c)
			if err != nil {
				b.Fatal(err)
			}
			defer sc.Close()
			// Warm up outside the timer so ns/op covers recorded requests only.
			if _, err := driver.Run(context.Background(), driver.Config{Concurrency: c.Concurrency, Requests: c.Concurrency * 10}, sc.Op); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			res, err := driver.Run(context.Background(), driver.Config{Concurrency: c.Concurrency, Requests: max(b.N, c.Concurrency)}, sc.Op)
			if err != nil {
				b.Fatal(err)
			}
			row := NewRow(c, res)
			b.ReportMetric(row.P50us, "p50-µs")
			b.ReportMetric(row.P99us, "p99-µs")
			b.ReportMetric(row.RPS, "req/s")
			if row.FirstChunkP50us > 0 {
				b.ReportMetric(row.FirstChunkP50us, "first-chunk-p50-µs")
			}
		})
	}
}
