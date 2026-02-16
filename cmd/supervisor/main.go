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

	srv := supervisor.NewServer(cfg, logger)

	if cfg.Server.HTTPPort > 0 {
		api := supervisor.NewHTTPAPI(nil, nil, nil, nil, cfg.Server.AuthToken, logger)
		srv.SetHTTPAPI(api)
		logger.Info("http api configured", zap.Int("http_port", cfg.Server.HTTPPort))
	}

	if err := srv.Start(); err != nil {
		logger.Error("failed to start server", zap.Error(err))
		os.Exit(1)
	}

	var discordBot *supervisor.DiscordBot
	if token := cfg.Channels.Discord.BotToken; token != "" {
		bot, botErr := supervisor.NewDiscordBot(
			token,
			cfg.Channels.Discord.GuildID,
			nil,
			srv.Hub(),
			nil,
			logger,
		)
		if botErr != nil {
			logger.Error("failed to create discord bot", zap.Error(botErr))
		} else if startErr := bot.Start(); startErr != nil {
			logger.Error("failed to start discord bot", zap.Error(startErr))
		} else {
			discordBot = bot
			logger.Info("discord bot started")
		}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigChan
	logger.Info("received signal, initiating graceful shutdown",
		zap.String("signal", sig.String()),
	)

	if discordBot != nil {
		if stopErr := discordBot.Stop(); stopErr != nil {
			logger.Error("error stopping discord bot", zap.Error(stopErr))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	if err := srv.Stop(); err != nil {
		logger.Error("error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	<-ctx.Done()

	logger.Info("supervisor exited cleanly")
	os.Exit(0)
}
