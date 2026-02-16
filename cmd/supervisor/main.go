package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
	"github.com/hal-o-swarm/hal-o-swarm/internal/supervisor"
	"go.uber.org/zap"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "./supervisor.config.json", "path to supervisor config file")
	flag.Parse()

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Load configuration
	cfg, err := config.LoadSupervisorConfig(*configPath)
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("config loaded successfully",
		zap.String("config_path", *configPath),
	)

	// Create and start server
	srv := supervisor.NewServer(cfg, logger)
	if err := srv.Start(); err != nil {
		logger.Error("failed to start server", zap.Error(err))
		os.Exit(1)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("received signal, initiating graceful shutdown",
		zap.String("signal", sig.String()),
	)

	// Graceful shutdown with context
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	// Stop the server
	if err := srv.Stop(); err != nil {
		logger.Error("error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	// Ensure context is done
	<-ctx.Done()

	logger.Info("supervisor exited cleanly")
	os.Exit(0)
}
