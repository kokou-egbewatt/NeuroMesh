// Command server runs the NeuroMesh runtime gRPC server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/logging"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/pkg/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "runtime:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := config.StringEnv("CONFIG_PATH", "configs/config.yaml")
	var cfg server.Config
	found, err := config.Load(configPath, &cfg)
	if err != nil {
		return err
	}
	cfg.Addr = config.StringEnv("RUNTIME_ADDR", cfg.Addr)

	log, err := logging.New(os.Stdout, "runtime", cfg.LogLevel)
	if err != nil {
		return err
	}
	if !found {
		log.Warn("config file not found, running on defaults and environment", "path", configPath)
	}

	srv, err := server.New(cfg, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return srv.Serve(ctx)
}
