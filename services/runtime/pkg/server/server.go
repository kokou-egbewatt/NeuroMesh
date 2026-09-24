// Package server builds and runs the runtime's gRPC server. cmd/server wires
// it to config and signals; tests boot it in-process on :0.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/stub"
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
	g := grpc.NewServer(
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgBytes),
		grpc.ChainUnaryInterceptor(unaryLogger(log)),
		grpc.ChainStreamInterceptor(streamLogger(log)),
	)
	neuromeshv1.RegisterInferenceServiceServer(g, stub.Server{})
	h := health.NewServer()
	h.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(g, h)
	if cfg.Reflection {
		log.Warn("grpc reflection is enabled")
		reflection.Register(g)
	}
	return &Server{cfg: cfg, log: log, grpc: g, health: h}, nil
}

// Listen binds the configured address. ctx bounds the bind only.
func (s *Server) Listen(ctx context.Context) error {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Addr, err)
	}
	s.lis = lis
	return nil
}

// Addr is the bound address; valid after Listen.
func (s *Server) Addr() net.Addr { return s.lis.Addr() }

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

func unaryLogger(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		log.LogAttrs(ctx, levelFor(err), "rpc",
			slog.String("rpc", info.FullMethod),
			slog.String("code", status.Code(err).String()),
			slog.String("caller", callerName(ctx)),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()))
		return resp, err
	}
}

func streamLogger(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		log.LogAttrs(ss.Context(), levelFor(err), "rpc",
			slog.String("rpc", info.FullMethod),
			slog.String("code", status.Code(err).String()),
			slog.String("caller", callerName(ss.Context())),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()))
		return err
	}
}

func levelFor(err error) slog.Level {
	if err != nil {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}
