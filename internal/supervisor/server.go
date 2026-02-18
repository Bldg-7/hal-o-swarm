package supervisor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"go.uber.org/zap"
)

// Server represents the supervisor daemon with lifecycle management.
type Server struct {
	cfg        *config.SupervisorConfig
	configPath string
	logger     *zap.Logger
	hub        *Hub
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	running    bool
	registry   *NodeRegistry

	httpAPI      *HTTPAPI
	httpShutdown func(ctx context.Context) error
	wsShutdown   func(ctx context.Context) error
	costs        *CostAggregator
	audit        *AuditLogger
	tlsConfig    *tls.Config
}

// NewServer creates a new supervisor server instance.
func NewServer(cfg *config.SupervisorConfig, logger *zap.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	origins := cfg.Server.AllowedOrigins
	if len(cfg.Security.OriginAllowlist) > 0 {
		origins = cfg.Security.OriginAllowlist
	}

	hub := NewHub(
		ctx,
		cfg.Server.AuthToken,
		origins,
		time.Duration(cfg.Server.HeartbeatIntervalSec)*time.Second,
		cfg.Server.HeartbeatTimeoutCount,
		logger,
	)

	if len(cfg.Security.OriginAllowlist) > 0 {
		hub.SetStrictOrigin(true)
	}

	return &Server{
		cfg:    cfg,
		logger: logger,
		hub:    hub,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *Server) SetTLSConfig(tlsCfg *tls.Config) {
	s.tlsConfig = tlsCfg
}

func (s *Server) SetAuditLogger(audit *AuditLogger) {
	s.audit = audit
}

func (s *Server) AuditLogger() *AuditLogger {
	return s.audit
}

func (s *Server) SetConfigPath(path string) {
	s.configPath = path
}

func (s *Server) RotateToken(newToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(newToken) < 32 {
		return fmt.Errorf("token must be at least 32 characters")
	}

	s.cfg.Server.AuthToken = newToken

	if s.hub != nil {
		s.hub.UpdateAuthToken(newToken)
	}

	s.logger.Info("auth token rotated successfully")
	return nil
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

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.logger.Info("supervisor started successfully",
		zap.Int("port", s.cfg.Server.Port),
	)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.hub.Run()
	}()

	s.wg.Add(1)
	go s.maintenanceLoop()

	if s.costs != nil {
		s.costs.Start(s.ctx)
	}

	wsAddr := fmt.Sprintf(":%d", s.cfg.Server.Port)
	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/", s.hub.ServeWS)
	wsSrv := &http.Server{
		Addr:         wsAddr,
		Handler:      wsMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info("websocket server starting", zap.String("addr", wsAddr))
		if err := wsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("websocket server error", zap.Error(err))
		}
	}()
	s.wsShutdown = wsSrv.Shutdown

	if s.httpAPI != nil && s.cfg.Server.HTTPPort > 0 {
		addr := fmt.Sprintf(":%d", s.cfg.Server.HTTPPort)
		httpSrv := &http.Server{
			Addr:         addr,
			Handler:      s.httpAPI.Handler(),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.logger.Info("http api server starting", zap.String("addr", addr))
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Error("http api server error", zap.Error(err))
			}
		}()
		s.httpShutdown = httpSrv.Shutdown
	}

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

	if s.wsShutdown != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.wsShutdown(shutdownCtx); err != nil {
			s.logger.Error("websocket server shutdown error", zap.Error(err))
		}
		shutdownCancel()
	}

	if s.httpShutdown != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.httpShutdown(shutdownCtx); err != nil {
			s.logger.Error("http api shutdown error", zap.Error(err))
		}
		shutdownCancel()
	}

	s.cancel()
	if s.costs != nil {
		s.costs.Stop()
	}

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

	if s.cfg.Security.TokenRotation.Enabled && s.configPath != "" {
		go s.tokenRotationLoop()
	}

	<-s.ctx.Done()

	s.logger.Debug("maintenance loop stopped")
}

func (s *Server) tokenRotationLoop() {
	interval := time.Duration(s.cfg.Security.TokenRotation.CheckIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			newCfg, err := config.LoadSupervisorConfig(s.configPath)
			if err != nil {
				s.logger.Debug("token rotation config reload skipped", zap.Error(err))
				continue
			}
			s.mu.Lock()
			currentToken := s.cfg.Server.AuthToken
			s.mu.Unlock()

			if newCfg.Server.AuthToken != currentToken {
				if err := s.RotateToken(newCfg.Server.AuthToken); err != nil {
					s.logger.Warn("token rotation failed", zap.Error(err))
				}
			}
		case <-s.ctx.Done():
			return
		}
	}
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

func (s *Server) Hub() *Hub {
	return s.hub
}

func (s *Server) SetRegistry(registry *NodeRegistry) {
	s.registry = registry
	if s.hub != nil && registry != nil {
		s.hub.ConfigureNodeRegistry(registry)
		s.hub.ConfigureCredentialReconciliation(registry, s.cfg.Credentials.Version)
	}
}

func (s *Server) SetHTTPAPI(api *HTTPAPI) {
	s.httpAPI = api
	if s.hub != nil {
		s.httpAPI.SetHub(s.hub)
	}
	if s.costs != nil {
		s.httpAPI.SetCostAggregator(s.costs)
	}
	hc := NewHealthChecker(nil, s.hub, nil, s.costs)
	s.httpAPI.SetHealthChecker(hc)
}

func (s *Server) SetCostAggregator(aggregator *CostAggregator) {
	s.costs = aggregator
	if s.httpAPI != nil {
		s.httpAPI.SetCostAggregator(aggregator)
	}
}
