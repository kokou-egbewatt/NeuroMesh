# middleware

Shared HTTP/gRPC middleware. `grpc/` has two client interceptors used by `sdk/go`:

- `grpc.DefaultDeadline(d)`: applies a deadline when the caller's context has none.
- `grpc.Retry(maxAttempts, backoff)`: retries idempotent unary calls on `Unavailable` only, with jittered backoff. `ResourceExhausted` is deliberately excluded: it means the request is too large or the backend is saturated, and a retry makes both worse. Not applied to streaming calls.
