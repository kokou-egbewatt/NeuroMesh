// Package sdk provides a thin typed client for the NeuroMesh InferenceService.
package sdk

import (
	"crypto/tls"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	mwgrpc "github.com/kokou-egbewatt/NeuroMesh/packages/middleware/grpc"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

// DefaultDeadline bounds unary calls (e.g. Route) that don't set their own
// context deadline. StreamInference is unaffected: the chain interceptors below
// only wrap unary calls.
const DefaultDeadline = 10 * time.Second

// ErrNoTransport is returned when NewClient is given neither WithTLS nor
// WithInsecure. Plaintext is never a default.
var ErrNoTransport = errors.New("sdk: choose a transport with WithTLS or WithInsecure")

// Client wraps a gRPC connection to services/runtime.
type Client struct {
	conn *grpc.ClientConn
	neuromeshv1.InferenceServiceClient
}

type options struct {
	tls         *tls.Config
	insecure    bool
	deadline    time.Duration
	maxAttempts uint
	dialOpts    []grpc.DialOption
}

// Option configures NewClient.
type Option func(*options)

// WithTLS dials with cfg. Set cfg.Certificates for mutual TLS.
func WithTLS(cfg *tls.Config) Option { return func(o *options) { o.tls = cfg } }

// WithInsecure dials in plaintext. For local runs with tls.insecure only.
func WithInsecure() Option { return func(o *options) { o.insecure = true } }

// WithDefaultDeadline overrides DefaultDeadline for unary calls without one.
func WithDefaultDeadline(d time.Duration) Option { return func(o *options) { o.deadline = d } }

// WithMaxAttempts sets the total attempts for a unary call on Unavailable.
func WithMaxAttempts(n uint) Option { return func(o *options) { o.maxAttempts = n } }

// WithDialOptions appends raw gRPC dial options.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) { o.dialOpts = append(o.dialOpts, opts...) }
}

// NewClient returns a Client for addr (host:port). The connection is lazy:
// nothing is dialed until the first call. Unary calls get a default deadline
// and are retried on Unavailable only; StreamInference is left untouched since
// a partially-consumed stream cannot be safely replayed.
func NewClient(addr string, opts ...Option) (*Client, error) {
	o := options{deadline: DefaultDeadline, maxAttempts: 3}
	for _, opt := range opts {
		opt(&o)
	}
	var creds credentials.TransportCredentials
	switch {
	case o.tls != nil && o.insecure:
		return nil, errors.New("sdk: WithTLS and WithInsecure are mutually exclusive")
	case o.tls != nil:
		creds = credentials.NewTLS(o.tls)
	case o.insecure:
		creds = insecure.NewCredentials()
	default:
		return nil, ErrNoTransport
	}
	dial := append([]grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(
			mwgrpc.DefaultDeadline(o.deadline),
			mwgrpc.Retry(o.maxAttempts, 100*time.Millisecond),
		),
	}, o.dialOpts...)
	conn, err := grpc.NewClient(addr, dial...)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, InferenceServiceClient: neuromeshv1.NewInferenceServiceClient(conn)}, nil
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
