package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"github.com/Bldg-7/hal-o-swarm/internal/storage"
	"github.com/Bldg-7/hal-o-swarm/internal/supervisor"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
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
	db, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		logger.Error("failed to open database", zap.Error(err))
		os.Exit(1)
	}
	defer db.Close()

	migrationRunner := storage.NewMigrationRunner(db)
	if err := migrationRunner.Migrate(); err != nil {
		logger.Error("failed to run migrations", zap.Error(err))
		os.Exit(1)
	}
	logger.Info("database migrations complete")

	registry := supervisor.NewNodeRegistry(db, logger)
	tracker := supervisor.NewSessionTracker(db, logger)
	dispatcher := supervisor.NewCommandDispatcher(db, registry, tracker, srv.Hub(), logger)
	audit := supervisor.NewAuditLogger(db, logger)

	srv.SetAuditLogger(audit)
	srv.SetRegistry(registry)

	supervisor.InitMetrics()
	logger.Info("metrics initialized")

	if cfg.Server.HTTPPort > 0 {
		api := supervisor.NewHTTPAPI(registry, tracker, dispatcher, db, cfg.Server.AuthToken, logger)
		api.SetAuditLogger(audit)
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
			dispatcher,
			srv.Hub(),
			tracker,
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

	if err := srv.Stop(); err != nil {
		logger.Error("error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("supervisor exited cleanly")
	os.Exit(0)
}
