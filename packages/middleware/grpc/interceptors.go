// Package grpc provides shared gRPC client interceptors: default deadlines
// and retry with backoff for idempotent unary calls.
package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// DefaultDeadline returns a unary client interceptor that applies d as the
// call deadline when the caller's context has none set. Callers that already
// set a deadline (e.g. per-request timeouts) are left untouched.
func DefaultDeadline(d time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
