// Command server runs the NeuroMesh gateway HTTPS entrypoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/logging"
	"github.com/kokou-egbewatt/NeuroMesh/packages/tracing"
	"github.com/kokou-egbewatt/NeuroMesh/services/gateway/pkg/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", config.StringEnv("CONFIG_PATH", "configs/config.yaml"),
		"config file; relative paths inside it resolve against its directory")
	flag.Parse()

	var cfg server.Config
	found, err := config.Load(*configPath, &cfg)
	if err != nil {
		return err
	}
	if found {
		base := filepath.Dir(*configPath)
		cfg.TLS.ResolveRelative(base)
		cfg.Runtime.TLS.ResolveRelative(base)
	}
	cfg.Addr = config.StringEnv("GATEWAY_ADDR", cfg.Addr)
	cfg.Runtime.Addr = config.StringEnv("RUNTIME_ADDR", cfg.Runtime.Addr)
	// The charts switch instrumentation on through the environment, so one image serves every setup.
	cfg.OTel.Endpoint = config.StringEnv("OTEL_EXPORTER_OTLP_ENDPOINT", cfg.OTel.Endpoint)
	cfg.OTel.Insecure = config.BoolEnv("OTEL_EXPORTER_OTLP_INSECURE", cfg.OTel.Insecure)
	if v, err := strconv.ParseFloat(os.Getenv("OTEL_TRACES_SAMPLER_ARG"), 64); err == nil {
		cfg.OTel.SampleRatio = v
	}
	cfg.Metrics.Enabled = config.BoolEnv("METRICS_ENABLED", cfg.Metrics.Enabled)
	cfg.Metrics.Addr = config.StringEnv("METRICS_ADDR", cfg.Metrics.Addr)

	log, err := logging.New(os.Stdout, "gateway", cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	if !found {
		log.Warn("config file not found, running on defaults and environment", "path", *configPath)
	}

	srv, err := server.New(cfg, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdown, err := tracing.Setup(ctx, "gateway", config.StringEnv("SERVICE_VERSION", "dev"), cfg.OTel)
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(flush)
	}()
	if cfg.OTel.Endpoint != "" {
		log.Info("tracing on", "endpoint", cfg.OTel.Endpoint)
	}
	return srv.Serve(ctx)
}
