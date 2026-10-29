// Package driver is a closed-loop load generator: a fixed number of workers
// each issue a fixed share of the requests back to back, and every latency is
// kept. Percentiles come from the sorted samples, exact rather than bucketed:
// at tens of thousands of requests per case the memory is a few hundred KiB,
// and a histogram library would pull its test dependencies into every
// workspace module through go work sync.
//
// Closed loop means a slow response delays the next request instead of queueing
// behind it, so the percentiles describe service time under the given
// concurrency, not the latency an open-loop client at a fixed rate would see
// (coordinated omission). For a stub baseline whose purpose is comparing
// overhead before and after a change, that is the right measurement.
package driver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"
)

// Sample is what one operation reports back.
type Sample struct {
	// FirstChunk is the time to the first streamed chunk; zero for unary calls.
	FirstChunk time.Duration
	// Chunks is how many chunks a stream delivered; zero for unary calls.
	Chunks int
}

// Op performs one request. The driver times it; the Sample adds what only the
// operation can see.
type Op func(ctx context.Context) (Sample, error)

// Config sets the shape of a run.
type Config struct {
	Concurrency int
	// Warmup requests run first and are discarded: TLS handshakes, connection
	// pools and the Go scheduler settle before anything is recorded.
	Warmup int
	// Requests is the fixed number of recorded requests, split evenly across
	// the workers. A fixed count, not a duration, keeps runs comparable.
	Requests int
}

// Stats summarizes one set of samples.
type Stats struct {
	P50, P95, P99, Max time.Duration
}

// Result is one run.
type Result struct {
	Requests   int
	Elapsed    time.Duration
	RPS        float64
	Latency    Stats
	FirstChunk *Stats
	// ChunksPerSec is the aggregate streamed chunk rate across all workers.
	ChunksPerSec float64
}

// Run executes cfg against op and fails on the first error: a baseline that
// includes failed requests measures the failure path, not the service.
func Run(ctx context.Context, cfg Config, op Op) (Result, error) {
	if cfg.Concurrency < 1 || cfg.Requests < cfg.Concurrency {
		return Result{}, fmt.Errorf("driver: need concurrency >= 1 and requests >= concurrency, got %d and %d", cfg.Concurrency, cfg.Requests)
	}
	if err := checkClock(); err != nil {
		return Result{}, err
	}
	if cfg.Warmup > 0 {
		if _, err := phase(ctx, cfg.Concurrency, cfg.Warmup, op); err != nil {
			return Result{}, fmt.Errorf("driver: warmup: %w", err)
		}
	}
	start := Now()
	workers, err := phase(ctx, cfg.Concurrency, cfg.Requests, op)
	elapsed := Since(start)
	if err != nil {
		return Result{}, err
	}

	var lat, first []time.Duration
	var chunks int64
	for _, w := range workers {
		lat = append(lat, w.latency...)
		first = append(first, w.firstChunk...)
		chunks += w.chunks
	}
	n := len(lat)
	res := Result{
		Requests: n,
		Elapsed:  elapsed,
		RPS:      float64(n) / elapsed.Seconds(),
		Latency:  stats(lat),
	}
	if len(first) > 0 {
		s := stats(first)
		res.FirstChunk = &s
		res.ChunksPerSec = float64(chunks) / elapsed.Seconds()
	}
	return res, nil
}

type worker struct {
	latency, firstChunk []time.Duration
	chunks              int64
}

// phase runs total requests across concurrency workers. The first error
// cancels the rest.
func phase(ctx context.Context, concurrency, total int, op Op) ([]*worker, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := make([]*worker, concurrency)
	errs := make([]error, concurrency)
	var wg sync.WaitGroup
	for i := range workers {
		share := total / concurrency
		if i < total%concurrency {
			share++
		}
		w := &worker{latency: make([]time.Duration, 0, share)}
		workers[i] = w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range share {
				if ctx.Err() != nil {
					return
				}
				t0 := Now()
				s, err := op(ctx)
				d := Since(t0)
				if err != nil {
					errs[i] = err
					cancel()
					return
				}
				w.latency = append(w.latency, d)
				if s.FirstChunk > 0 {
					w.firstChunk = append(w.firstChunk, s.FirstChunk)
				}
				w.chunks += int64(s.Chunks)
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return workers, ctx.Err()
}

// stats sorts samples in place and reads nearest-rank percentiles.
func stats(samples []time.Duration) Stats {
	slices.Sort(samples)
	q := func(p float64) time.Duration {
		i := int(math.Ceil(p / 100 * float64(len(samples))))
		return samples[max(i-1, 0)]
	}
	return Stats{P50: q(50), P95: q(95), P99: q(99), Max: samples[len(samples)-1]}
}
