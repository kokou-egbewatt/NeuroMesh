package admit

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/backend"
)

// fake counts what reaches it. A request whose prompt is "hold" waits on gate; "poison" fails.
type fake struct {
	gate  chan struct{}
	now   atomic.Int32
	peak  atomic.Int32
	calls atomic.Int32
	mu    sync.Mutex
	order []string
}

func newFake() *fake { return &fake{gate: make(chan struct{})} }

func (*fake) Name() string { return "fake" }

func (f *fake) enter(r backend.Request) {
	f.calls.Add(1)
	n := f.now.Add(1)
	for p := f.peak.Load(); n > p && !f.peak.CompareAndSwap(p, n); p = f.peak.Load() {
	}
	f.mu.Lock()
	f.order = append(f.order, r.Prompt)
	f.mu.Unlock()
}

func (f *fake) Complete(ctx context.Context, r backend.Request) (backend.Response, error) {
	f.enter(r)
	defer f.now.Add(-1)
	switch r.Prompt {
	case "hold":
		select {
		case <-f.gate:
		case <-ctx.Done():
			return backend.Response{}, ctx.Err()
		}
	case "poison":
		return backend.Response{}, fmt.Errorf("fake: %w", backend.ErrInvalid)
	case "sleep":
		time.Sleep(2 * time.Millisecond)
	}
	return backend.Response{Output: "out:" + r.Prompt}, nil
}

func (f *fake) Stream(_ context.Context, r backend.Request, emit func(string) error) (backend.Usage, error) {
	f.enter(r)
	defer f.now.Add(-1)
	for _, tok := range []string{"a", "b"} {
		if err := emit(tok); err != nil {
			return backend.Usage{}, err
		}
	}
	return backend.Usage{CompletionTokens: 2}, nil
}

func req(p string) backend.Request { return backend.Request{Model: "m", Prompt: p} }

// waitLoad blocks until the cap shows inFlight and queued.
func waitLoad(t *testing.T, b *Backend, inFlight, queued int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		i, q := b.Load()
		if i == inFlight && q == queued {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("load %d in flight, %d queued; want %d, %d", i, q, inFlight, queued)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		cfg  Config
		want string
	}{
		{Config{MaxInFlight: 1}, ""},
		{Config{MaxInFlight: 120, MaxQueue: 64}, ""},
		{Config{MaxInFlight: 0}, "batching.max_in_flight"},
		{Config{MaxInFlight: 4, MaxQueue: -1}, "batching.max_queue"},
	} {
		err := tc.cfg.Validate()
		if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%+v: got %v, want %q", tc.cfg, err, tc.want)
		}
	}
}

// 32 concurrent requests on a cap of 8: the backend never sees more than 8, and all 32 complete.
func TestBackendNeverSeesMoreThanTheCap(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 8, MaxQueue: 32})
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Go(func() {
			if _, err := b.Complete(context.Background(), req("sleep")); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := f.calls.Load(); got != 32 {
		t.Errorf("%d calls, want 32", got)
	}
	if got := f.peak.Load(); got != 8 {
		t.Errorf("peak %d in flight, want exactly 8", got)
	}
	waitLoad(t, b, 0, 0)
}

// With the cap and queue full, the next request fails at once and never reaches the backend.
func TestFullQueueRejectsAtOnce(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1, MaxQueue: 1})
	held := make(chan error, 2)
	go func() { _, err := b.Complete(context.Background(), req("hold")); held <- err }()
	waitLoad(t, b, 1, 0)
	go func() { _, err := b.Complete(context.Background(), req("queued")); held <- err }()
	waitLoad(t, b, 1, 1)

	start := time.Now()
	_, err := b.Complete(context.Background(), req("rejected"))
	if !errors.Is(err, backend.ErrSaturated) || !strings.Contains(err.Error(), "fake") {
		t.Fatalf("got %v, want ErrSaturated naming the backend", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("reject took %v", d)
	}
	if got := testutil.ToFloat64(b.rejected); got != 1 {
		t.Errorf("rejected counter %v, want 1", got)
	}
	close(f.gate)
	for range 2 {
		if err := <-held; err != nil {
			t.Error(err)
		}
	}
	if slices.Contains(f.order, "rejected") {
		t.Error("the rejected request reached the backend")
	}
}

// MaxQueue 0: a request that finds the cap full is rejected, never queued.
func TestNoQueueRejectsWhenFull(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1})
	done := make(chan struct{})
	go func() { _, _ = b.Complete(context.Background(), req("hold")); close(done) }()
	waitLoad(t, b, 1, 0)
	if _, err := b.Complete(context.Background(), req("x")); !errors.Is(err, backend.ErrSaturated) {
		t.Fatalf("got %v, want ErrSaturated", err)
	}
	close(f.gate)
	<-done
}

// A queued request that is cancelled, or whose deadline passes, leaves the queue without a slot, and
// the requests behind it still run.
func TestQueuedRequestLeavesOnCancelAndDeadline(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1, MaxQueue: 3})
	first := make(chan error, 1)
	go func() { _, err := b.Complete(context.Background(), req("hold")); first <- err }()
	waitLoad(t, b, 1, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() { _, err := b.Complete(ctx, req("cancelled")); cancelled <- err }()
	waitLoad(t, b, 1, 1)
	dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer dcancel()
	expired := make(chan error, 1)
	go func() { _, err := b.Complete(dctx, req("expired")); expired <- err }()
	waitLoad(t, b, 1, 2)
	last := make(chan error, 1)
	go func() { _, err := b.Complete(context.Background(), req("last")); last <- err }()
	waitLoad(t, b, 1, 3)

	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if err := <-expired; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired: %v", err)
	}
	waitLoad(t, b, 1, 1)
	close(f.gate)
	if err := <-first; err != nil {
		t.Error(err)
	}
	if err := <-last; err != nil {
		t.Error(err)
	}
	if want := []string{"hold", "last"}; !slices.Equal(f.order, want) {
		t.Errorf("backend saw %v, want %v", f.order, want)
	}
	waitLoad(t, b, 0, 0)
}

// An already-cancelled request takes nothing.
func TestCancelledBeforeAcquire(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Complete(ctx, req("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if f.calls.Load() != 0 {
		t.Error("reached the backend")
	}
}

// Queued requests are admitted in arrival order.
func TestQueueIsFIFO(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1, MaxQueue: 5})
	var wg sync.WaitGroup
	wg.Go(func() { _, _ = b.Complete(context.Background(), req("hold")) })
	waitLoad(t, b, 1, 0)
	want := []string{"hold"}
	for i := range 5 {
		p := fmt.Sprint(i)
		want = append(want, p)
		wg.Go(func() { _, _ = b.Complete(context.Background(), req(p)) })
		waitLoad(t, b, 1, i+1)
	}
	close(f.gate)
	wg.Wait()
	if !slices.Equal(f.order, want) {
		t.Errorf("order %v, want %v", f.order, want)
	}
}

// A stream holds its slot until it returns; then the slot is free again.
func TestStreamHoldsItsSlot(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1})
	inside := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		var toks []string
		u, err := b.Stream(context.Background(), req("s"), func(tok string) error {
			if len(toks) == 0 {
				close(inside)
				<-resume
			}
			toks = append(toks, tok)
			return nil
		})
		if err == nil && (u.CompletionTokens != 2 || strings.Join(toks, "") != "ab") {
			err = fmt.Errorf("stream got %q %+v", toks, u)
		}
		done <- err
	}()
	<-inside
	if _, err := b.Complete(context.Background(), req("x")); !errors.Is(err, backend.ErrSaturated) {
		t.Fatalf("during the stream: %v, want ErrSaturated", err)
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := b.Complete(context.Background(), req("after")); err != nil {
		t.Fatalf("after the stream: %v", err)
	}
}

// A stream that finds the cap full is rejected before it starts.
func TestStreamRejectedWhenFull(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 1})
	done := make(chan struct{})
	go func() { _, _ = b.Complete(context.Background(), req("hold")); close(done) }()
	waitLoad(t, b, 1, 0)
	_, err := b.Stream(context.Background(), req("s"), func(string) error { return nil })
	if !errors.Is(err, backend.ErrSaturated) {
		t.Fatalf("got %v", err)
	}
	close(f.gate)
	<-done
}

// A failed request frees its slot and fails only its own caller.
func TestFailedRequestReleasesItsSlot(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 2, MaxQueue: 8})
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range 8 {
		p := "ok"
		if i == 3 {
			p = "poison"
		}
		wg.Go(func() { _, results[i] = b.Complete(context.Background(), req(p)) })
	}
	wg.Wait()
	for i, err := range results {
		if (i == 3) != errors.Is(err, backend.ErrInvalid) || (i != 3 && err != nil) {
			t.Errorf("request %d: %v", i, err)
		}
	}
	waitLoad(t, b, 0, 0)
}

// Many callers, random cancellations: the cap holds and every slot comes back. Run with -race.
func TestCapHoldsUnderChurn(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 4, MaxQueue: 16})
	var wg sync.WaitGroup
	for range 400 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(rand.IntN(3000))*time.Microsecond)
			defer cancel()
			_, _ = b.Complete(ctx, req("sleep"))
		})
	}
	wg.Wait()
	if p := f.peak.Load(); p > 4 {
		t.Errorf("peak %d, cap 4", p)
	}
	waitLoad(t, b, 0, 0)
}

// Below the cap a request is not held: p99 of the added time is far under a millisecond.
func TestLoneRequestIsNotHeld(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 8, MaxQueue: 8})
	const n = 2000
	direct, capped := make([]time.Duration, n), make([]time.Duration, n)
	for i := range n {
		s := time.Now()
		_, _ = f.Complete(context.Background(), req("x"))
		direct[i] = time.Since(s)
		s = time.Now()
		_, _ = b.Complete(context.Background(), req("x"))
		capped[i] = time.Since(s)
	}
	p99 := func(d []time.Duration) time.Duration {
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[n*99/100]
	}
	if added := p99(capped) - p99(direct); added > time.Millisecond {
		t.Errorf("p99 added %v, want under 1ms", added)
	}
}

func TestMetrics(t *testing.T) {
	f := newFake()
	b := New(f, Config{MaxInFlight: 2, MaxQueue: 1})
	reg := prometheus.NewRegistry()
	reg.MustRegister(b.Collectors()...)
	done := make(chan struct{}, 3)
	for range 3 {
		go func() { _, _ = b.Complete(context.Background(), req("hold")); done <- struct{}{} }()
	}
	waitLoad(t, b, 2, 1)
	want := `
# HELP neuromesh_backend_in_flight Requests running on the backend.
# TYPE neuromesh_backend_in_flight gauge
neuromesh_backend_in_flight{backend="fake",service="runtime"} 2
# HELP neuromesh_backend_max_in_flight The backend's cap.
# TYPE neuromesh_backend_max_in_flight gauge
neuromesh_backend_max_in_flight{backend="fake",service="runtime"} 2
# HELP neuromesh_backend_queued Requests waiting for a backend slot.
# TYPE neuromesh_backend_queued gauge
neuromesh_backend_queued{backend="fake",service="runtime"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want),
		"neuromesh_backend_in_flight", "neuromesh_backend_queued", "neuromesh_backend_max_in_flight"); err != nil {
		t.Error(err)
	}
	close(f.gate)
	for range 3 {
		<-done
	}
	if got := testutil.CollectAndCount(b.wait); got != 1 {
		t.Errorf("wait histogram series %d", got)
	}
	if n := histCount(t, reg); n != 3 {
		t.Errorf("queue wait observations %d, want 3", n)
	}
}

func histCount(t *testing.T, reg *prometheus.Registry) uint64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "neuromesh_backend_queue_wait_seconds" {
			return mf.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func BenchmarkUncontended(b *testing.B) {
	capped := New(newFake(), Config{MaxInFlight: 8})
	for b.Loop() {
		_, _ = capped.Complete(context.Background(), req("x"))
	}
}
