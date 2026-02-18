package supervisor

import (
	"testing"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"go.uber.org/zap"
)

func TestServerLifecycle(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.SupervisorConfig{
		Server: struct {
			Port                  int      `json:"port"`
			HTTPPort              int      `json:"http_port"`
			AuthToken             string   `json:"auth_token"`
			HeartbeatIntervalSec  int      `json:"heartbeat_interval_sec"`
			HeartbeatTimeoutCount int      `json:"heartbeat_timeout_count"`
			AllowedOrigins        []string `json:"allowed_origins"`
		}{
			Port:                  9999,
			AuthToken:             "test-token",
			HeartbeatIntervalSec:  30,
			HeartbeatTimeoutCount: 3,
		},
		Cost: config.CostConfig{
			PollIntervalMinutes: 5,
		},
	}

	srv := NewServer(cfg, logger)

	if srv.IsRunning() {
		t.Error("server should not be running initially")
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if !srv.IsRunning() {
		t.Error("server should be running after Start()")
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	if srv.IsRunning() {
		t.Error("server should not be running after Stop()")
	}
}

func TestServerDoubleStart(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.SupervisorConfig{
		Server: struct {
			Port                  int      `json:"port"`
			HTTPPort              int      `json:"http_port"`
			AuthToken             string   `json:"auth_token"`
			HeartbeatIntervalSec  int      `json:"heartbeat_interval_sec"`
			HeartbeatTimeoutCount int      `json:"heartbeat_timeout_count"`
			AllowedOrigins        []string `json:"allowed_origins"`
		}{
			Port:                  9998,
			AuthToken:             "test-token",
			HeartbeatIntervalSec:  30,
			HeartbeatTimeoutCount: 3,
		},
		Cost: config.CostConfig{
			PollIntervalMinutes: 5,
		},
	}

	srv := NewServer(cfg, logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("first Start() failed: %v", err)
	}
	defer srv.Stop()

	if err := srv.Start(); err == nil {
		t.Error("second Start() should fail")
	}
}

func TestServerDoubleStop(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.SupervisorConfig{
		Server: struct {
			Port                  int      `json:"port"`
			HTTPPort              int      `json:"http_port"`
			AuthToken             string   `json:"auth_token"`
			HeartbeatIntervalSec  int      `json:"heartbeat_interval_sec"`
			HeartbeatTimeoutCount int      `json:"heartbeat_timeout_count"`
			AllowedOrigins        []string `json:"allowed_origins"`
		}{
			Port:                  9997,
			AuthToken:             "test-token",
			HeartbeatIntervalSec:  30,
			HeartbeatTimeoutCount: 3,
		},
		Cost: config.CostConfig{
			PollIntervalMinutes: 5,
		},
	}

	srv := NewServer(cfg, logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("first Stop() failed: %v", err)
	}

	if err := srv.Stop(); err == nil {
		t.Error("second Stop() should fail")
	}
}

func TestServerContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.SupervisorConfig{
		Server: struct {
			Port                  int      `json:"port"`
			HTTPPort              int      `json:"http_port"`
			AuthToken             string   `json:"auth_token"`
			HeartbeatIntervalSec  int      `json:"heartbeat_interval_sec"`
			HeartbeatTimeoutCount int      `json:"heartbeat_timeout_count"`
			AllowedOrigins        []string `json:"allowed_origins"`
		}{
			Port:                  9996,
			AuthToken:             "test-token",
			HeartbeatIntervalSec:  30,
			HeartbeatTimeoutCount: 3,
		},
		Cost: config.CostConfig{
			PollIntervalMinutes: 5,
		},
	}

	srv := NewServer(cfg, logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	ctx := srv.Context()

	select {
	case <-ctx.Done():
		t.Error("context should not be cancelled yet")
	default:
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Error("context should be cancelled after Stop()")
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.SupervisorConfig{
		Server: struct {
			Port                  int      `json:"port"`
			HTTPPort              int      `json:"http_port"`
			AuthToken             string   `json:"auth_token"`
			HeartbeatIntervalSec  int      `json:"heartbeat_interval_sec"`
			HeartbeatTimeoutCount int      `json:"heartbeat_timeout_count"`
			AllowedOrigins        []string `json:"allowed_origins"`
		}{
			Port:                  9995,
			AuthToken:             "test-token",
			HeartbeatIntervalSec:  30,
			HeartbeatTimeoutCount: 3,
		},
		Cost: config.CostConfig{
			PollIntervalMinutes: 5,
		},
	}

	srv := NewServer(cfg, logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Stop() took too long")
	}
}
