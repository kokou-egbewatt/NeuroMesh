// Package admit caps the requests in flight on a backend, with a bounded queue in front (#2). The
// backend batches on the GPU itself; past the size of its KV cache it preempts and queues without
// limit, so throughput stays flat while every request slows down. The cap keeps it below that point
// and a full queue rejects at once with backend.ErrSaturated.
package admit

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/backend"
)

// Config sizes the cap. For vLLM, MaxInFlight is about the KV cache's tokens (its startup log) over
// prompt plus max_tokens per request.
type Config struct {
	MaxInFlight int
	// MaxQueue is how many requests may wait for a slot; 0 rejects whatever finds the cap full.
	MaxQueue int
}

// Validate rejects a cap that can't admit anything.
func (c Config) Validate() error {
	if c.MaxInFlight < 1 {
		return fmt.Errorf("batching.max_in_flight %d: want at least 1", c.MaxInFlight)
	}
	if c.MaxQueue < 0 {
		return fmt.Errorf("batching.max_queue %d: want 0 or more", c.MaxQueue)
	}
	return nil
}

// Backend is a backend.Backend behind the cap. A stream holds its slot until it returns.
type Backend struct {
	next backend.Backend
	cfg  Config

	mu       sync.Mutex
	inFlight int
	// waiters are the queued requests in arrival order; release hands a slot to the front one by
	// closing its channel.
	waiters list.List

	wait     prometheus.Histogram
	rejected prometheus.Counter
	gauges   []prometheus.Collector
}

// New caps next. cfg must be valid.
func New(next backend.Backend, cfg Config) *Backend {
	b := &Backend{next: next, cfg: cfg}
	labels := prometheus.Labels{"service": "runtime", "backend": next.Name()}
	b.wait = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "neuromesh_backend_queue_wait_seconds",
		Help:        "Time a request waited for a backend slot, admitted requests only.",
		ConstLabels: labels,
		Buckets:     []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	})
	b.rejected = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "neuromesh_backend_rejected_total",
		Help:        "Requests rejected because the backend's cap and queue were full.",
		ConstLabels: labels,
	})
	b.gauges = []prometheus.Collector{
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "neuromesh_backend_in_flight", Help: "Requests running on the backend.", ConstLabels: labels,
		}, func() float64 { n, _ := b.Load(); return float64(n) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "neuromesh_backend_queued", Help: "Requests waiting for a backend slot.", ConstLabels: labels,
		}, func() float64 { _, q := b.Load(); return float64(q) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "neuromesh_backend_max_in_flight", Help: "The backend's cap.", ConstLabels: labels,
		}, func() float64 { return float64(cfg.MaxInFlight) }),
	}
	return b
}

// Collectors are the cap's metrics, for the service's registry.
func (b *Backend) Collectors() []prometheus.Collector {
	return append([]prometheus.Collector{b.wait, b.rejected}, b.gauges...)
}

// Load is the requests in flight and queued now.
func (b *Backend) Load() (inFlight, queued int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight, b.waiters.Len()
}

// Name is the capped backend's.
func (b *Backend) Name() string { return b.next.Name() }

// Complete runs req once a slot is free.
func (b *Backend) Complete(ctx context.Context, req backend.Request) (backend.Response, error) {
	if err := b.acquire(ctx); err != nil {
		return backend.Response{}, err
	}
	defer b.release()
	return b.next.Complete(ctx, req)
}

// Stream runs req once a slot is free and holds the slot until the stream ends.
func (b *Backend) Stream(ctx context.Context, req backend.Request, emit func(string) error) (backend.Usage, error) {
	if err := b.acquire(ctx); err != nil {
		return backend.Usage{}, err
	}
	defer b.release()
	return b.next.Stream(ctx, req, emit)
}

func (b *Backend) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	if b.inFlight < b.cfg.MaxInFlight && b.waiters.Len() == 0 {
		b.inFlight++
		b.mu.Unlock()
		b.wait.Observe(0)
		return nil
	}
	if b.waiters.Len() >= b.cfg.MaxQueue {
		b.mu.Unlock()
		b.rejected.Inc()
		return fmt.Errorf("%s: %d in flight, %d queued: %w", b.next.Name(), b.cfg.MaxInFlight, b.cfg.MaxQueue, backend.ErrSaturated)
	}
	ready := make(chan struct{})
	e := b.waiters.PushBack(ready)
	b.mu.Unlock()

	start := time.Now()
	select {
	case <-ready:
		b.wait.Observe(time.Since(start).Seconds())
		return nil
	case <-ctx.Done():
	}
	b.mu.Lock()
	select {
	case <-ready:
		// release handed this request a slot as it gave up: pass it on.
		b.mu.Unlock()
		b.release()
	default:
		b.waiters.Remove(e)
		b.mu.Unlock()
	}
	return ctx.Err()
}

// release frees a slot, or hands it straight to the oldest waiter so nothing can take it in between.
func (b *Backend) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.waiters.Front(); e != nil {
		b.waiters.Remove(e)
		close(e.Value.(chan struct{}))
		return
	}
	b.inFlight--
}
