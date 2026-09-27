// Command server runs the NeuroMesh gateway HTTPS entrypoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/logging"
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
	return srv.Serve(ctx)
}
