package supervisor

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
	"go.uber.org/zap"
)

// Server represents the supervisor daemon with lifecycle management.
type Server struct {
	cfg     *config.SupervisorConfig
	logger  *zap.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

// NewServer creates a new supervisor server instance.
func NewServer(cfg *config.SupervisorConfig, logger *zap.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:    cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start initializes and starts the supervisor server.
// It returns an error if startup fails.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}
	s.mu.Unlock()

	s.logger.Info("supervisor starting",
		zap.Int("port", s.cfg.Server.Port),
		zap.Int("heartbeat_interval_sec", s.cfg.Server.HeartbeatIntervalSec),
	)

	// Verify we can bind to the configured port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Server.Port))
	if err != nil {
		s.logger.Error("failed to bind to port", zap.Error(err))
		return fmt.Errorf("failed to bind to port %d: %w", s.cfg.Server.Port, err)
	}
	defer listener.Close()

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.logger.Info("supervisor started successfully",
		zap.Int("port", s.cfg.Server.Port),
	)

	// Start background maintenance goroutine
	s.wg.Add(1)
	go s.maintenanceLoop()

	return nil
}

// Stop gracefully shuts down the supervisor server.
// It cancels the context and waits for all goroutines to finish.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is not running")
	}
	s.mu.Unlock()

	s.logger.Info("supervisor shutting down gracefully")

	// Signal all goroutines to stop
	s.cancel()

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("supervisor shutdown complete")
	case <-s.ctx.Done():
		s.logger.Warn("supervisor shutdown timeout exceeded")
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	return nil
}

// maintenanceLoop runs background maintenance tasks.
// It monitors the context for cancellation.
func (s *Server) maintenanceLoop() {
	defer s.wg.Done()

	s.logger.Debug("maintenance loop started")

	<-s.ctx.Done()

	s.logger.Debug("maintenance loop stopped")
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Context returns the server's context for cancellation propagation.
func (s *Server) Context() context.Context {
	return s.ctx
}
