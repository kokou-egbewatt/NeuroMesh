package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/utils/certgen"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fixture struct {
	dir string
	ca  *certgen.CA
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ca, err := certgen.NewCA("test-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{dir: t.TempDir(), ca: ca}
}

func (f fixture) tls(t *testing.T, cn string) config.TLS {
	t.Helper()
	leaf, err := f.ca.Issue(cn, []string{"localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.dir, cn)
	if err := certgen.WriteSecretLayout(dir, leaf, f.ca.CertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return config.TLS{CertFile: filepath.Join(dir, "tls.crt"), KeyFile: filepath.Join(dir, "tls.key"), CAFile: filepath.Join(dir, "ca.crt")}
}

// start boots a runtime on :0 and returns its address and a stop func that
// runs the real shutdown path.
func start(t *testing.T, cfg Config) (string, func()) {
	t.Helper()
	cfg.Addr = "127.0.0.1:0"
	s, err := New(cfg, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Listen(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	stop := func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	}
	t.Cleanup(func() { cancel() })
	return s.Addr().String(), stop
}

func dial(t *testing.T, addr string, creds credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func mtls(t *testing.T, c config.TLS) credentials.TransportCredentials {
	t.Helper()
	cfg, err := c.ClientConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	return credentials.NewTLS(cfg)
}

func route(conn *grpc.ClientConn, prompt string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := neuromeshv1.NewInferenceServiceClient(conn).Route(ctx, &neuromeshv1.RouteRequest{Model: "m", Prompt: prompt})
	return err
}

func TestMutualTLSCallSucceeds(t *testing.T) {
	f := newFixture(t)
	srv := f.tls(t, "runtime")
	srv.AllowedClientNames = []string{"gateway"}
	addr, _ := start(t, Config{TLS: srv})
	if err := route(dial(t, addr, mtls(t, f.tls(t, "gateway"))), "hi"); err != nil {
		t.Fatalf("mTLS call failed: %v", err)
	}
}

func TestPlaintextDialIsRejected(t *testing.T) {
	f := newFixture(t)
	addr, _ := start(t, Config{TLS: f.tls(t, "runtime")})
	err := route(dial(t, addr, insecure.NewCredentials()), "hi")
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("plaintext call: %v, want Unavailable from a failed handshake", err)
	}
}

func TestDisallowedCallerIsRejected(t *testing.T) {
	f := newFixture(t)
	srv := f.tls(t, "runtime")
	srv.AllowedClientNames = []string{"gateway"}
	addr, _ := start(t, Config{TLS: srv})
	if err := route(dial(t, addr, mtls(t, f.tls(t, "intruder"))), "hi"); err == nil {
		t.Fatal("a certificate outside allowed_client_names was served")
	}
}

func TestExpiredClientCertificateIsRejected(t *testing.T) {
	f := newFixture(t)
	addr, _ := start(t, Config{TLS: f.tls(t, "runtime")})
	past := time.Now().Add(-2 * time.Hour)
	leaf, err := f.ca.IssueBetween("gateway", []string{"localhost"}, past, past.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.dir, "expired")
	if err := certgen.WriteSecretLayout(dir, leaf, f.ca.CertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	old := config.TLS{CertFile: filepath.Join(dir, "tls.crt"), KeyFile: filepath.Join(dir, "tls.key"), CAFile: filepath.Join(dir, "ca.crt")}
	if err := route(dial(t, addr, mtls(t, old)), "hi"); err == nil {
		t.Fatal("an expired client certificate was served")
	}
}

func TestMessageOverLimitIsResourceExhausted(t *testing.T) {
	addr, _ := start(t, Config{TLS: config.TLS{Insecure: true}, MaxRecvMsgBytes: 1024})
	err := route(dial(t, addr, insecure.NewCredentials()), strings.Repeat("a", 4096))
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized message: %v, want ResourceExhausted", err)
	}
}

func TestShutdownFlipsHealthAndStops(t *testing.T) {
	addr, stop := start(t, Config{TLS: config.TLS{Insecure: true}, ShutdownTimeout: time.Second})
	conn := dial(t, addr, insecure.NewCredentials())
	hc := healthpb.NewHealthClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := hc.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health before shutdown: %v %v", resp, err)
	}
	stop()
	if err := route(conn, "hi"); status.Code(err) != codes.Unavailable {
		t.Fatalf("call after shutdown: %v, want Unavailable", err)
	}
}

func TestReflectionIsOffByDefault(t *testing.T) {
	addr, _ := start(t, Config{TLS: config.TLS{Insecure: true}})
	conn := dial(t, addr, insecure.NewCredentials())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}, "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo")
	if err == nil {
		_ = stream.CloseSend()
		err = stream.RecvMsg(new(healthpb.HealthCheckResponse))
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("reflection call: %v, want Unimplemented", err)
	}
}
