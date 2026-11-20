// Package server builds and runs the runtime's gRPC server. cmd/server wires
// it to config and signals; tests boot it in-process on :0.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/telemetry"
	"github.com/kokou-egbewatt/NeuroMesh/packages/tracing"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/admit"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/backend"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/service"
)

// Config is the runtime's config file.
type Config struct {
	Addr     string `yaml:"addr"`
	LogLevel string `yaml:"log_level"`
	// LogFormat is json (default) or text; local configs use text.
	LogFormat string `yaml:"log_format"`
	// ShutdownTimeout bounds GracefulStop; after it, in-flight calls are cut.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	// MaxRecvMsgBytes must stay above the gateway's limits.max_body_bytes, or
	// requests the gateway accepted fail here with ResourceExhausted.
	MaxRecvMsgBytes int        `yaml:"max_recv_msg_bytes"`
	Reflection      bool       `yaml:"reflection"`
	TLS             config.TLS `yaml:"tls"`
	// HealthAddr, when set, also serves grpc.health.v1 in plaintext there and nothing else, for
	// probes that can't present a client certificate (kubelet's grpc probe). It reports the same
	// status as the main port, NOT_SERVING on shutdown included.
	HealthAddr string `yaml:"health_addr"`
	// OTel exports spans to the collector gateway; empty endpoint, no tracing (#10).
	OTel tracing.Config `yaml:"otel"`
	// Metrics serves Prometheus /metrics on its own port when enabled (#10).
	Metrics telemetry.Config `yaml:"metrics"`
	// Backend is what generates the text (#1).
	Backend BackendConfig `yaml:"backend"`
	// Batching caps the requests in flight on the backend (#2).
	Batching BatchingConfig `yaml:"batching"`
}

// BatchingConfig caps the backend. The backend batches what is in flight on the GPU itself; the
// cap keeps that below the size of its KV cache, where it would start preempting. Off by default.
type BatchingConfig struct {
	Enabled bool `yaml:"enabled"`
	// MaxInFlight is the most requests on the backend at once; for vLLM, about its KV cache tokens
	// over prompt plus max_tokens.
	MaxInFlight int `yaml:"max_in_flight"`
	// MaxQueue is how many more may wait for a slot; past it a request gets ResourceExhausted at once.
	MaxQueue int `yaml:"max_queue"`
}

// BackendConfig picks the backend. The stub is the default, so the tests and the #13 baseline run
// without a model server.
type BackendConfig struct {
	// Kind is stub, ollama or vllm; anything else fails at startup.
	Kind   string      `yaml:"kind"`
	Ollama HTTPBackend `yaml:"ollama"`
	VLLM   HTTPBackend `yaml:"vllm"`
}

// HTTPBackend is the server of an HTTP backend kind.
type HTTPBackend struct {
	BaseURL string `yaml:"base_url"`
	// DialTimeout bounds connecting (default 3s): an unreachable server is Unavailable within it.
	DialTimeout time.Duration `yaml:"dial_timeout"`
	// ResponseHeaderTimeout caps the wait for an answer to start (default 60s). A unary generation
	// starts answering only when it is finished, so this bounds model load plus generation.
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
}

func (h HTTPBackend) http() backend.HTTPConfig {
	return backend.HTTPConfig{BaseURL: h.BaseURL, DialTimeout: h.DialTimeout, ResponseHeaderTimeout: h.ResponseHeaderTimeout}
}

// url is the chosen backend's server, for the startup log.
func (b BackendConfig) url() string {
	if b.Kind == backend.KindVLLM {
		return b.VLLM.BaseURL
	}
	return b.Ollama.BaseURL
}

// Defaults fills unset fields.
func (c *Config) Defaults() {
	if c.Addr == "" {
		c.Addr = ":50051"
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	if c.MaxRecvMsgBytes <= 0 {
		c.MaxRecvMsgBytes = 8 << 20
	}
}

// Server is a runtime gRPC server.
type Server struct {
	cfg    Config
	log    *slog.Logger
	grpc   *grpc.Server
	health *health.Server
	lis    net.Listener
	// probe serves only the health service, in plaintext, on HealthAddr; nil when unset.
	probe    *grpc.Server
	probeLis net.Listener
	// metrics is nil when metrics are off; Observe on nil does nothing.
	metrics *telemetry.Metrics
}

// New builds the server. It requires client certificates unless tls.insecure.
func New(cfg Config, log *slog.Logger) (*Server, error) {
	cfg.Defaults()
	var creds credentials.TransportCredentials
	if cfg.TLS.Insecure {
		log.Warn("tls.insecure is set: serving plaintext gRPC, never do this outside a local run")
		creds = insecure.NewCredentials()
	} else {
		tlsCfg, err := cfg.TLS.ServerConfig(true)
		if err != nil {
			return nil, err
		}
		creds = credentials.NewTLS(tlsCfg)
	}
	var m *telemetry.Metrics
	if cfg.Metrics.Enabled {
		m = telemetry.New("runtime")
	}
	g := grpc.NewServer(
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgBytes),
		// A server span per call, continuing the trace the gateway sends in the metadata. Health
		// checks are left out: they are probes, not requests.
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(notHealth))),
		grpc.ChainUnaryInterceptor(unaryLogger(log, m)),
		grpc.ChainStreamInterceptor(streamLogger(log, m)),
	)
	b, err := backend.New(backend.Config{Kind: cfg.Backend.Kind, Ollama: cfg.Backend.Ollama.http(), VLLM: cfg.Backend.VLLM.http()})
	if err != nil {
		return nil, err
	}
	if b.Name() != backend.KindStub {
		log.Info("backend selected", "backend", b.Name(), "backend_url", cfg.Backend.url())
	}
	if cfg.Batching.Enabled {
		ac := admit.Config{MaxInFlight: cfg.Batching.MaxInFlight, MaxQueue: cfg.Batching.MaxQueue}
		if err := ac.Validate(); err != nil {
			return nil, err
		}
		capped := admit.New(b, ac)
		m.Register(capped.Collectors()...)
		b = capped
		log.Info("batching on", "max_in_flight", ac.MaxInFlight, "max_queue", ac.MaxQueue)
	}
	neuromeshv1.RegisterInferenceServiceServer(g, service.New(b))
	h := health.NewServer()
	h.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(g, h)
	if cfg.Reflection {
		log.Warn("grpc reflection is enabled")
		reflection.Register(g)
	}
	s := &Server{cfg: cfg, log: log, grpc: g, health: h, metrics: m}
	if cfg.HealthAddr != "" {
		s.probe = grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
		healthpb.RegisterHealthServer(s.probe, h)
	}
	return s, nil
}

// Listen binds the configured address, and HealthAddr when set. ctx bounds the bind only.
func (s *Server) Listen(ctx context.Context) error {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Addr, err)
	}
	if s.probe != nil {
		plis, err := lc.Listen(ctx, "tcp", s.cfg.HealthAddr)
		if err != nil {
			_ = lis.Close()
			return fmt.Errorf("listen on health_addr %s: %w", s.cfg.HealthAddr, err)
		}
		s.probeLis = plis
	}
	s.lis = lis
	return nil
}

// Addr is the bound address; valid after Listen.
func (s *Server) Addr() net.Addr { return s.lis.Addr() }

// HealthAddr is the bound plaintext health address, or nil when health_addr is unset.
func (s *Server) HealthAddr() net.Addr {
	if s.probeLis == nil {
		return nil
	}
	return s.probeLis.Addr()
}

// Serve runs until ctx is done, then reports NOT_SERVING, drains in-flight
// calls for up to ShutdownTimeout, and cuts whatever is left.
func (s *Server) Serve(ctx context.Context) error {
	if s.lis == nil {
		if err := s.Listen(ctx); err != nil {
			return err
		}
	}
	errc := make(chan error, 1)
	go func() { errc <- s.grpc.Serve(s.lis) }()
	s.log.Info("runtime listening", "addr", s.lis.Addr().String(), "tls", !s.cfg.TLS.Insecure)
	go func() {
		if err := telemetry.Serve(ctx, s.cfg.Metrics, s.metrics, s.log); err != nil {
			s.log.Error("metrics server stopped", "err", err)
		}
	}()
	if s.probe != nil {
		go func() { _ = s.probe.Serve(s.probeLis) }()
		s.log.Info("health listening", "addr", s.probeLis.Addr().String(), "tls", false)
		// Keep answering NOT_SERVING while the main server drains, so the pod leaves the
		// endpoints first; stopped last.
		defer s.probe.Stop()
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	s.log.Info("runtime shutting down", "timeout", s.cfg.ShutdownTimeout.String())
	s.health.Shutdown()
	stopped := make(chan struct{})
	go func() { s.grpc.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(s.cfg.ShutdownTimeout):
		s.log.Warn("graceful stop timed out, closing remaining calls")
		s.grpc.Stop()
		<-stopped
	}
	if err := <-errc; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}

func callerName(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	if ti, ok := p.AuthInfo.(credentials.TLSInfo); ok && len(ti.State.PeerCertificates) > 0 {
		return ti.State.PeerCertificates[0].Subject.CommonName
	}
	return ""
}

// requestIDer is every request message that carries the gateway's request_id.
type requestIDer interface{ GetRequestId() string }

func requestID(req any) string {
	if r, ok := req.(requestIDer); ok {
		return r.GetRequestId()
	}
	return ""
}

// notHealth keeps health checks out of tracing: they are probes, and would bury the requests.
func notHealth(info *stats.RPCTagInfo) bool {
	return !strings.HasPrefix(info.FullMethodName, "/grpc.health.v1.Health/")
}

func unaryLogger(log *slog.Logger, m *telemetry.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)
		if !strings.HasPrefix(info.FullMethod, "/grpc.health.v1.Health/") {
			m.Observe(info.FullMethod, code.String(), time.Since(start), code != codes.OK)
		}
		attrs := []slog.Attr{
			slog.String("rpc", info.FullMethod),
			slog.String("code", code.String()),
			slog.String("caller", callerName(ctx)),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		}
		if id := requestID(req); id != "" {
			attrs = append(attrs, slog.String("request_id", id))
		}
		log.LogAttrs(ctx, levelFor(err), "rpc", attrs...)
		return resp, err
	}
}

// countingStream counts the messages a streaming handler sends and when the first one went out, for
// the span (#10): a stream is judged by its first chunk and its length, not by its end.
type countingStream struct {
	grpc.ServerStream
	start  time.Time
	chunks int
	first  time.Duration
	reqID  string
}

func (s *countingStream) SendMsg(msg any) error {
	if s.chunks == 0 {
		s.first = time.Since(s.start)
	}
	s.chunks++
	return s.ServerStream.SendMsg(msg)
}

func (s *countingStream) RecvMsg(msg any) error {
	err := s.ServerStream.RecvMsg(msg)
	if err == nil && s.reqID == "" {
		s.reqID = requestID(msg)
	}
	return err
}

func streamLogger(log *slog.Logger, m *telemetry.Metrics) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		cs := &countingStream{ServerStream: ss, start: time.Now()}
		err := handler(srv, cs)
		code := status.Code(err)
		// The span otelgrpc opened ends when the stream closes, after this; these land on it.
		if span := trace.SpanFromContext(ss.Context()); span.IsRecording() {
			span.SetAttributes(attribute.Int("neuromesh.stream.chunks", cs.chunks))
			if cs.chunks > 0 {
				span.SetAttributes(attribute.Float64("neuromesh.stream.first_chunk_ms", float64(cs.first.Microseconds())/1000))
			}
		}
		if !strings.HasPrefix(info.FullMethod, "/grpc.health.v1.Health/") {
			m.Observe(info.FullMethod, code.String(), time.Since(cs.start), code != codes.OK)
		}
		attrs := []slog.Attr{
			slog.String("rpc", info.FullMethod),
			slog.String("code", code.String()),
			slog.String("caller", callerName(ss.Context())),
			slog.Int("chunks", cs.chunks),
			slog.Int64("duration_ms", time.Since(cs.start).Milliseconds()),
		}
		if cs.reqID != "" {
			attrs = append(attrs, slog.String("request_id", cs.reqID))
		}
		log.LogAttrs(ss.Context(), levelFor(err), "rpc", attrs...)
		return err
	}
}

func levelFor(err error) slog.Level {
	if err != nil {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

// Metrics is the server's request metrics, nil when metrics are off.
func (s *Server) Metrics() *telemetry.Metrics { return s.metrics }
