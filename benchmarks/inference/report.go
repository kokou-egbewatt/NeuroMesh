package inference

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/benchmarks/inference/driver"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

// Case is one row of the matrix.
type Case struct {
	Scenario    string
	Concurrency int
}

// Matrix is what the baseline records: the unary path at 1, 8 and 64
// concurrent clients and the stream at 1 and 8.
var Matrix = []Case{
	{"route", 1},
	{"route", 8},
	{"route", 64},
	{"stream", 1},
	{"stream", 8},
}

// Row is one measured case, in microseconds so the JSON reads without units.
type Row struct {
	Scenario    string  `json:"scenario"`
	Concurrency int     `json:"concurrency"`
	Requests    int     `json:"requests"`
	P50us       float64 `json:"p50_us"`
	P95us       float64 `json:"p95_us"`
	P99us       float64 `json:"p99_us"`
	MaxUs       float64 `json:"max_us"`
	RPS         float64 `json:"rps"`
	// Stream rows only.
	FirstChunkP50us float64 `json:"first_chunk_p50_us,omitempty"`
	FirstChunkP99us float64 `json:"first_chunk_p99_us,omitempty"`
	ChunksPerSec    float64 `json:"chunks_per_sec,omitempty"`
}

// Key identifies a row across runs.
func (r Row) Key() string { return fmt.Sprintf("%s/c=%d", r.Scenario, r.Concurrency) }

// RunCase measures one case against a running stack.
func RunCase(ctx context.Context, s *harness.Stack, c Case, warmup, requests int) (Row, error) {
	sc, err := NewScenario(s, c)
	if err != nil {
		return Row{}, err
	}
	defer sc.Close()
	res, err := driver.Run(ctx, driver.Config{Concurrency: c.Concurrency, Warmup: warmup, Requests: requests}, sc.Op)
	if err != nil {
		return Row{}, fmt.Errorf("%s/c=%d: %w", c.Scenario, c.Concurrency, err)
	}
	return NewRow(c, res), nil
}

// NewScenario builds the scenario a case names.
func NewScenario(s *harness.Stack, c Case) (Scenario, error) {
	switch c.Scenario {
	case "route":
		return Route(s, c.Concurrency), nil
	case "stream":
		return Stream(s)
	default:
		return Scenario{}, fmt.Errorf("unknown scenario %q", c.Scenario)
	}
}

// NewRow converts a driver result.
func NewRow(c Case, res driver.Result) Row {
	r := Row{
		Scenario:    c.Scenario,
		Concurrency: c.Concurrency,
		Requests:    res.Requests,
		P50us:       us(res.Latency.P50),
		P95us:       us(res.Latency.P95),
		P99us:       us(res.Latency.P99),
		MaxUs:       us(res.Latency.Max),
		RPS:         math.Round(res.RPS),
	}
	if res.FirstChunk != nil {
		r.FirstChunkP50us = us(res.FirstChunk.P50)
		r.FirstChunkP99us = us(res.FirstChunk.P99)
		r.ChunksPerSec = math.Round(res.ChunksPerSec)
	}
	return r
}

func us(d time.Duration) float64 { return math.Round(float64(d)/float64(time.Microsecond)*10) / 10 }

// WriteMarkdown writes the rows as the table BASELINE.md carries.
func WriteMarkdown(w io.Writer, rows []Row) error {
	if _, err := fmt.Fprintln(w, "| Scenario | Concurrency | Requests | p50 (µs) | p95 (µs) | p99 (µs) | max (µs) | req/s | first chunk p50 / p99 (µs) | chunks/s |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |"); err != nil {
		return err
	}
	for _, r := range rows {
		first, chunks := "n/a", "n/a"
		if r.FirstChunkP50us > 0 {
			first = fmt.Sprintf("%.1f / %.1f", r.FirstChunkP50us, r.FirstChunkP99us)
			chunks = fmt.Sprintf("%.0f", r.ChunksPerSec)
		}
		if _, err := fmt.Fprintf(w, "| %s | %d | %d | %.1f | %.1f | %.1f | %.1f | %.0f | %s | %s |\n",
			r.Scenario, r.Concurrency, r.Requests, r.P50us, r.P95us, r.P99us, r.MaxUs, r.RPS, first, chunks); err != nil {
			return err
		}
	}
	return nil
}

// Drift is one row's p50 change between two runs.
type Drift struct {
	Key    string
	A, B   float64
	Change float64 // (B - A) / A
}

// Compare pairs rows by Key and returns every p50 drift plus the rows whose
// drift reaches tolerance (0.10 is the repeatability bar). A row present in
// only one run is an error: the runs did not measure the same thing.
func Compare(a, b []Row, tolerance float64) (all, over []Drift, err error) {
	bm := make(map[string]Row, len(b))
	for _, r := range b {
		bm[r.Key()] = r
	}
	if len(a) != len(b) {
		return nil, nil, fmt.Errorf("runs have %d and %d rows", len(a), len(b))
	}
	for _, ra := range a {
		rb, ok := bm[ra.Key()]
		if !ok {
			return nil, nil, fmt.Errorf("row %s is missing from the second run", ra.Key())
		}
		if ra.P50us <= 0 {
			return nil, nil, fmt.Errorf("row %s has no p50 in the first run", ra.Key())
		}
		d := Drift{Key: ra.Key(), A: ra.P50us, B: rb.P50us, Change: (rb.P50us - ra.P50us) / ra.P50us}
		all = append(all, d)
		if math.Abs(d.Change) >= tolerance {
			over = append(over, d)
		}
	}
	return all, over, nil
}
