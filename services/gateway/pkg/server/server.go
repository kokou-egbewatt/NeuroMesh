// Package server builds and runs the gateway's HTTPS server and its client to
// the runtime. cmd/server wires it to config and signals; tests boot it
// in-process on :0.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	sdk "github.com/kokou-egbewatt/NeuroMesh/sdk/go"
	"github.com/kokou-egbewatt/NeuroMesh/services/gateway/internal/httpapi"
	"github.com/kokou-egbewatt/NeuroMesh/services/gateway/internal/router"
)

// Config is the gateway's config file.
type Config struct {
	Addr            string         `yaml:"addr"`
	LogLevel        string         `yaml:"log_level"`
	ShutdownTimeout time.Duration  `yaml:"shutdown_timeout"`
	TLS             config.TLS     `yaml:"tls"`
	HTTP            HTTPConfig     `yaml:"http"`
	Limits          httpapi.Limits `yaml:"limits"`
	Runtime         RuntimeConfig  `yaml:"runtime"`
}

// HTTPConfig bounds how long a client may hold a connection doing nothing.
type HTTPConfig struct {
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout"`
	MaxHeaderBytes    int           `yaml:"max_header_bytes"`
}

// RuntimeConfig is the gateway's client to services/runtime.
type RuntimeConfig struct {
	Addr string `yaml:"addr"`
	// ServerName must match a SAN on the runtime's certificate.
	ServerName string     `yaml:"server_name"`
	TLS        config.TLS `yaml:"tls"`
}

// Defaults fills unset fields.
func (c *Config) Defaults() {
	if c.Addr == "" {
		c.Addr = ":8443"
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		c.HTTP.ReadHeaderTimeout = 5 * time.Second
	}
	if c.HTTP.ReadTimeout <= 0 {
		c.HTTP.ReadTimeout = 30 * time.Second
	}
	if c.HTTP.IdleTimeout <= 0 {
		c.HTTP.IdleTimeout = 120 * time.Second
	}
	if c.HTTP.MaxHeaderBytes <= 0 {
		c.HTTP.MaxHeaderBytes = 64 << 10
	}
	if c.Runtime.Addr == "" {
		c.Runtime.Addr = "localhost:50051"
	}
	if c.Runtime.ServerName == "" {
		c.Runtime.ServerName = "runtime"
	}
	c.Limits.Defaults()
}

// Server is the gateway.
type Server struct {
	cfg    Config
	log    *slog.Logger
	client *sdk.Client
	http   *http.Server
	lis    net.Listener
}

// New builds the gateway: an HTTPS listener and a mutual TLS client to the
// runtime. Either side runs plaintext only with its own tls.insecure set.
func New(cfg Config, log *slog.Logger) (*Server, error) {
	cfg.Defaults()

	var transport sdk.Option
	if cfg.Runtime.TLS.Insecure {
		log.Warn("runtime.tls.insecure is set: dialing the runtime in plaintext, never do this outside a local run")
		transport = sdk.WithInsecure()
	} else {
		clientTLS, err := cfg.Runtime.TLS.ClientConfig(cfg.Runtime.ServerName)
		if err != nil {
			return nil, fmt.Errorf("runtime client: %w", err)
		}
		transport = sdk.WithTLS(clientTLS)
	}
	client, err := sdk.NewClient(cfg.Runtime.Addr, transport)
	if err != nil {
		return nil, err
	}
	rt, err := router.NewStatic(client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	srv := &http.Server{
		Handler:           httpapi.New(rt, cfg.Limits, log),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	if cfg.TLS.Insecure {
		log.Warn("tls.insecure is set: serving plaintext HTTP, never do this outside a local run")
	} else {
		serverTLS, err := cfg.TLS.ServerConfig(false)
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("https listener: %w", err)
		}
		srv.TLSConfig = serverTLS
	}
	return &Server{cfg: cfg, log: log, client: client, http: srv}, nil
}

// Listen binds the configured address and wraps it in TLS unless insecure.
// ctx bounds the bind only.
func (s *Server) Listen(ctx context.Context) error {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Addr, err)
	}
	if s.http.TLSConfig != nil {
		lis = tls.NewListener(lis, s.http.TLSConfig)
	}
	s.lis = lis
	return nil
}

// Addr is the bound address; valid after Listen.
func (s *Server) Addr() net.Addr { return s.lis.Addr() }

// Serve runs until ctx is done, then drains for up to ShutdownTimeout and
// closes the runtime client.
func (s *Server) Serve(ctx context.Context) error {
	if s.lis == nil {
		if err := s.Listen(ctx); err != nil {
			return err
		}
	}
	defer func() { _ = s.client.Close() }()
	errc := make(chan error, 1)
	go func() { errc <- s.http.Serve(s.lis) }()
	s.log.Info("gateway listening", "addr", s.lis.Addr().String(), "https", s.http.TLSConfig != nil, "runtime", s.cfg.Runtime.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	s.log.Info("gateway shutting down", "timeout", s.cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
