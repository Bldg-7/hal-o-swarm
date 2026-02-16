package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hal-o-swarm/hal-o-swarm/internal/agent"
	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
)

func main() {
	configPath := flag.String("config", "./agent.config.json", "path to agent config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadAgentConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Create agent
	ag, err := agent.NewAgent(cfg)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start agent
	ctx := context.Background()
	if err := ag.Start(ctx); err != nil {
		log.Fatalf("failed to start agent: %v", err)
	}

	fmt.Printf("Agent started with %d projects\n", ag.GetRegistry().ProjectCount())
	for _, proj := range ag.GetRegistry().ListProjects() {
		fmt.Printf("  - %s: %s\n", proj.Name, proj.Directory)
	}

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("\nShutting down agent...")

	if err := ag.Stop(ctx); err != nil {
		log.Fatalf("failed to stop agent: %v", err)
	}

	fmt.Println("Agent stopped")
}
