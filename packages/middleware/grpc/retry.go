package grpc

import (
	"time"

	grpcretry "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// RetryableCodes are the status codes Retry acts on. Unavailable only: it is
// the one code that means "this attempt never reached a handler that could
// have done the work". ResourceExhausted is deliberately absent, because the
// runtime returns it for a message over its size limit and a saturated backend
// returns it under load; retrying either sends the same oversized or
// unwelcome request again and multiplies the load it complains about.
var RetryableCodes = []codes.Code{codes.Unavailable}

// Retry returns a unary client interceptor that retries RetryableCodes with
// jittered exponential backoff, up to maxAttempts calls in total.
//
// Only use this on idempotent unary RPCs. Server-streaming RPCs are not
// retried here since a partially-consumed stream cannot be safely replayed.
func Retry(maxAttempts uint, backoff time.Duration) grpc.UnaryClientInterceptor {
	return grpcretry.UnaryClientInterceptor(
		grpcretry.WithMax(maxAttempts),
		grpcretry.WithBackoff(grpcretry.BackoffExponentialWithJitter(backoff, 0.1)),
		grpcretry.WithCodes(RetryableCodes...),
	)
}
