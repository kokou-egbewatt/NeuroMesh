package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func countingInvoker(code codes.Code, calls *int) grpc.UnaryInvoker {
	return func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		*calls++
		if code == codes.OK {
			return nil
		}
		return status.Error(code, "scripted")
	}
}

func TestRetryOnlyRetriesUnavailable(t *testing.T) {
	cases := []struct {
		code      codes.Code
		wantCalls int
	}{
		{codes.Unavailable, 3},
		{codes.ResourceExhausted, 1},
		{codes.InvalidArgument, 1},
		{codes.Internal, 1},
		{codes.OK, 1},
	}
	for _, tc := range cases {
		t.Run(tc.code.String(), func(t *testing.T) {
			calls := 0
			err := Retry(3, time.Millisecond)(context.Background(), "/svc/M", nil, nil, nil, countingInvoker(tc.code, &calls))
			if calls != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tc.wantCalls)
			}
			if got := status.Code(err); got != tc.code {
				t.Fatalf("code = %v, want %v", got, tc.code)
			}
		})
	}
}

func TestDefaultDeadlineAppliesOnlyWhenAbsent(t *testing.T) {
	var got time.Time
	var had bool
	inv := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		got, had = ctx.Deadline()
		return nil
	}
	i := DefaultDeadline(time.Minute)

	_ = i(context.Background(), "/m", nil, nil, nil, inv)
	if !had || time.Until(got) > time.Minute || time.Until(got) < 50*time.Second {
		t.Fatalf("default deadline not applied: had=%v in=%v", had, time.Until(got))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = i(ctx, "/m", nil, nil, nil, inv)
	if time.Until(got) > time.Second {
		t.Fatal("caller's shorter deadline was overridden")
	}
}
