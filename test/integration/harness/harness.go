// Package harness boots the runtime and the gateway in-process on real
// loopback sockets over real TLS (HTTPS at the edge, mutual TLS between the
// two). The integration tests and the benchmarks share it so both measure the
// same wiring.
package harness

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/telemetry"
	"github.com/kokou-egbewatt/NeuroMesh/packages/utils/certgen"
	sdk "github.com/kokou-egbewatt/NeuroMesh/sdk/go"
	gateway "github.com/kokou-egbewatt/NeuroMesh/services/gateway/pkg/server"
	runtime "github.com/kokou-egbewatt/NeuroMesh/services/runtime/pkg/server"
)

// Options configures Start. The zero value is valid.
type Options struct {
	// Logger receives both services' logs. Nil discards them.
	Logger *slog.Logger
	// Metrics turns on both services' request metrics, on ephemeral ports; Stack.Metrics reads them.
	Metrics bool
	// Backend is the runtime's backend; the zero value is the stub.
	Backend runtime.BackendConfig
}

// Stack is a running runtime and gateway pair.
type Stack struct {
	GatewayURL  string
	RuntimeAddr string
	CAPEM       []byte
	// Certs holds the "runtime" and "gateway" leaves, each with the CA.
	Certs map[string]config.TLS
	// Metrics holds the gateway's and the runtime's request metrics when Options.Metrics is set.
	Metrics map[string]*telemetry.Metrics
	// ServerName is the name the gateway's certificate is checked against; empty means
	// "localhost", the in-process stack's.
	ServerName string

	dir         string
	stopGateway func()
	stopRuntime func()
}

// Start mints a throwaway CA and leaves, then boots the runtime and the
// gateway on 127.0.0.1 with ephemeral ports. Close releases everything.
func Start(ctx context.Context, opts Options) (_ *Stack, err error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	dir, err := os.MkdirTemp("", "neuromesh-harness-")
	if err != nil {
		return nil, err
	}
	s := &Stack{dir: dir, stopGateway: notStarted, stopRuntime: notStarted}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()

	ca, err := certgen.NewCA("integration-ca", time.Hour)
	if err != nil {
		return nil, err
	}
	s.CAPEM = ca.CertPEM
	s.Certs = map[string]config.TLS{}
	for _, cn := range []string{"runtime", "gateway"} {
		if s.Certs[cn], err = issue(ca, dir, cn); err != nil {
			return nil, err
		}
	}

	rtTLS := s.Certs["runtime"]
	rtTLS.AllowedClientNames = []string{"gateway"}
	metrics := telemetry.Config{Enabled: opts.Metrics, Addr: "127.0.0.1:0"}
	rt, err := runtime.New(runtime.Config{Addr: "127.0.0.1:0", TLS: rtTLS, ShutdownTimeout: time.Second, Metrics: metrics, Backend: opts.Backend}, log)
	if err != nil {
		return nil, err
	}
	if err := rt.Listen(ctx); err != nil {
		return nil, err
	}
	s.stopRuntime = serve(rt.Serve)
	s.RuntimeAddr = rt.Addr().String()

	gwEdge := s.Certs["gateway"]
	gw, err := gateway.New(gateway.Config{
		Addr:            "127.0.0.1:0",
		ShutdownTimeout: time.Second,
		TLS:             config.TLS{CertFile: gwEdge.CertFile, KeyFile: gwEdge.KeyFile},
		Runtime:         gateway.RuntimeConfig{Addr: s.RuntimeAddr, ServerName: "runtime", TLS: s.Certs["gateway"]},
		Metrics:         metrics,
	}, log)
	if err != nil {
		return nil, err
	}
	if err := gw.Listen(ctx); err != nil {
		return nil, err
	}
	s.stopGateway = serve(gw.Serve)
	s.GatewayURL = "https://" + gw.Addr().String()
	if opts.Metrics {
		s.Metrics = map[string]*telemetry.Metrics{"gateway": gw.Metrics(), "runtime": rt.Metrics()}
	}
	return s, nil
}

// Boot is Start for tests and benchmarks: it fails tb on error and closes the
// stack in tb's cleanup.
func Boot(tb testing.TB) *Stack { return BootWith(tb, Options{}) }

// BootWith is Boot with options.
func BootWith(tb testing.TB, opts Options) *Stack {
	tb.Helper()
	s, err := Start(context.Background(), opts)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := s.Close(); err != nil {
			tb.Error(err)
		}
	})
	return s
}

// Remote is a stack that is already running somewhere else, such as the cluster-deployed gateway
// behind the local edge (#14): only the gateway's URL and the CA its certificate chains to. The
// runtime is out of reach, so RuntimeAddr is empty and SDKClient fails; Close does nothing.
func Remote(gatewayURL string, caPEM []byte) (*Stack, error) {
	u, err := url.Parse(gatewayURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil, fmt.Errorf("remote gateway %q: want an https:// URL", gatewayURL)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(caPEM) {
		return nil, errors.New("remote gateway: no certificate in the CA PEM")
	}
	return &Stack{
		GatewayURL:  strings.TrimSuffix(gatewayURL, "/"),
		CAPEM:       caPEM,
		ServerName:  u.Hostname(),
		stopGateway: notStarted,
		stopRuntime: notStarted,
	}, nil
}

// StopRuntime stops the runtime and leaves the gateway up, for tests of the
// gateway's behavior with its backend gone. It is safe to call more than once.
func (s *Stack) StopRuntime() { s.stopRuntime() }

// Close stops the gateway, then the runtime, then removes the certificates.
func (s *Stack) Close() error {
	s.stopGateway()
	s.stopRuntime()
	if s.dir == "" {
		return nil
	}
	return os.RemoveAll(s.dir)
}

// HTTPSClient trusts the stack's CA and speaks TLS 1.3 to the gateway.
func (s *Stack) HTTPSClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: s.Transport(0)}
}

// Transport is the HTTPS transport HTTPSClient uses, with maxIdle idle
// connections kept per host so concurrent callers reuse them (0 keeps the
// net/http default).
func (s *Stack) Transport(maxIdle int) *http.Transport {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(s.CAPEM)
	name := s.ServerName
	if name == "" {
		name = "localhost"
	}
	return &http.Transport{
		TLSClientConfig:     &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13, ServerName: name},
		MaxIdleConns:        maxIdle,
		MaxIdleConnsPerHost: maxIdle,
	}
}

// SDKClient dials the runtime directly, presenting the gateway's certificate.
func (s *Stack) SDKClient(opts ...sdk.Option) (*sdk.Client, error) {
	if s.RuntimeAddr == "" {
		return nil, errors.New("this stack's runtime is not reachable from here")
	}
	clientTLS, err := s.Certs["gateway"].ClientConfig("runtime")
	if err != nil {
		return nil, err
	}
	return sdk.NewClient(s.RuntimeAddr, append([]sdk.Option{sdk.WithTLS(clientTLS)}, opts...)...)
}

func issue(ca *certgen.CA, dir, cn string) (config.TLS, error) {
	leaf, err := ca.Issue(cn, []string{cn, "localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		return config.TLS{}, err
	}
	d := filepath.Join(dir, cn)
	if err := certgen.WriteSecretLayout(d, leaf, ca.CertPEM, 0o600); err != nil {
		return config.TLS{}, err
	}
	return config.TLS{CertFile: filepath.Join(d, "tls.crt"), KeyFile: filepath.Join(d, "tls.key"), CAFile: filepath.Join(d, "ca.crt")}, nil
}

// notStarted stands in for the stop function of a server Start never reached,
// so Close can run after a partial boot.
func notStarted() {
	// Nothing is running, so there is nothing to stop.
}

// serve runs fn until the returned stop is called, then waits for it to return.
func serve(fn func(context.Context) error) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = fn(ctx)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}
