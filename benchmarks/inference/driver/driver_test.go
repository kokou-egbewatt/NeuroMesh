package driver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRecordsExactlyTheRequestedCount(t *testing.T) {
	var calls atomic.Int64
	op := func(context.Context) (Sample, error) {
		calls.Add(1)
		return Sample{}, nil
	}
	res, err := Run(context.Background(), Config{Concurrency: 7, Warmup: 5, Requests: 100}, op)
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 100 {
		t.Fatalf("recorded %d requests, want 100", res.Requests)
	}
	if got := calls.Load(); got != 105 {
		t.Fatalf("op ran %d times, want 105 (warmup plus recorded)", got)
	}
	if res.FirstChunk != nil {
		t.Fatal("unary run reported a first-chunk histogram")
	}
}

func TestRunReportsStreamFields(t *testing.T) {
	op := func(context.Context) (Sample, error) {
		return Sample{FirstChunk: 2 * time.Millisecond, Chunks: 3}, nil
	}
	res, err := Run(context.Background(), Config{Concurrency: 2, Requests: 10}, op)
	if err != nil {
		t.Fatal(err)
	}
	if res.FirstChunk == nil || res.FirstChunk.P50 < time.Millisecond {
		t.Fatalf("first chunk stats missing or wrong: %+v", res.FirstChunk)
	}
	if res.ChunksPerSec <= 0 {
		t.Fatal("chunk rate not reported")
	}
}

func TestRunFailsOnTheFirstError(t *testing.T) {
	boom := errors.New("boom")
	var calls atomic.Int64
	op := func(context.Context) (Sample, error) {
		if calls.Add(1) == 3 {
			return Sample{}, boom
		}
		return Sample{}, nil
	}
	if _, err := Run(context.Background(), Config{Concurrency: 1, Requests: 1000}, op); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("op ran %d times after failing, want it to stop at 3", calls.Load())
	}
}

func TestRunRejectsAnEmptyShape(t *testing.T) {
	op := func(context.Context) (Sample, error) { return Sample{}, nil }
	if _, err := Run(context.Background(), Config{Concurrency: 8, Requests: 4}, op); err == nil {
		t.Fatal("fewer requests than workers was accepted")
	}
}
