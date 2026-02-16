package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSupervisorConfigExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "supervisor.config.example.json")
	cfg, err := LoadSupervisorConfig(examplePath)
	if err != nil {
		t.Fatalf("failed to load example supervisor config: %v", err)
	}
	if cfg.Server.Port != 8420 {
		t.Errorf("expected port 8420, got %d", cfg.Server.Port)
	}
	if cfg.Server.AuthToken == "" {
		t.Error("expected auth_token to be set")
	}
}

func TestLoadAgentConfigExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "agent.config.example.json")
	cfg, err := LoadAgentConfig(examplePath)
	if err != nil {
		t.Fatalf("failed to load example agent config: %v", err)
	}
	if cfg.SupervisorURL == "" {
		t.Error("expected supervisor_url to be set")
	}
	if len(cfg.Projects) == 0 {
		t.Error("expected projects array to be non-empty")
	}
}

func TestLoadEnvManifestExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "env-manifest.example.json")
	manifest, err := LoadEnvManifest(examplePath)
	if err != nil {
		t.Fatalf("failed to load example env manifest: %v", err)
	}
	if manifest.Version == "" {
		t.Error("expected version to be set")
	}
	if len(manifest.Requirements) == 0 {
		t.Error("expected requirements array to be non-empty")
	}
}

func TestSupervisorConfigValidationInvalidPort(t *testing.T) {
	cfg := &SupervisorConfig{}
	cfg.Server.Port = 0
	cfg.Server.AuthToken = "token"
	cfg.Server.HeartbeatIntervalSec = 30
	cfg.Server.HeartbeatTimeoutCount = 3

	err := validateSupervisorConfig(cfg)
	if err == nil {
		t.Error("expected error for invalid port, got nil")
	}
	if err.Error() != "validation error: server.port must be between 1 and 65535, got 0" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSupervisorConfigValidationMissingAuthToken(t *testing.T) {
	cfg := &SupervisorConfig{}
	cfg.Server.Port = 8420
	cfg.Server.AuthToken = ""
	cfg.Server.HeartbeatIntervalSec = 30
	cfg.Server.HeartbeatTimeoutCount = 3

	err := validateSupervisorConfig(cfg)
	if err == nil {
		t.Error("expected error for missing auth token, got nil")
	}
	if err.Error() != "validation error: server.auth_token is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSupervisorConfigValidationInvalidHeartbeatInterval(t *testing.T) {
	cfg := &SupervisorConfig{}
	cfg.Server.Port = 8420
	cfg.Server.AuthToken = "token"
	cfg.Server.HeartbeatIntervalSec = 0
	cfg.Server.HeartbeatTimeoutCount = 3

	err := validateSupervisorConfig(cfg)
	if err == nil {
		t.Error("expected error for invalid heartbeat interval, got nil")
	}
	if err.Error() != "validation error: server.heartbeat_interval_sec must be positive, got 0" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentConfigValidationMissingSupervisorURL(t *testing.T) {
	cfg := &AgentConfig{
		SupervisorURL: "",
		AuthToken:     "token",
		OpencodePort:  4096,
	}

	err := validateAgentConfig(cfg)
	if err == nil {
		t.Error("expected error for missing supervisor URL, got nil")
	}
	if err.Error() != "validation error: supervisor_url is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentConfigValidationMissingAuthToken(t *testing.T) {
	cfg := &AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "",
		OpencodePort:  4096,
	}

	err := validateAgentConfig(cfg)
	if err == nil {
		t.Error("expected error for missing auth token, got nil")
	}
	if err.Error() != "validation error: auth_token is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentConfigValidationInvalidPort(t *testing.T) {
	cfg := &AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "token",
		OpencodePort:  70000,
	}

	err := validateAgentConfig(cfg)
	if err == nil {
		t.Error("expected error for invalid port, got nil")
	}
	if err.Error() != "validation error: opencode_port must be between 1 and 65535, got 70000" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentConfigValidationEmptyProjects(t *testing.T) {
	cfg := &AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "token",
		OpencodePort:  4096,
	}
	cfg.Projects = nil

	err := validateAgentConfig(cfg)
	if err == nil {
		t.Error("expected error for empty projects, got nil")
	}
	if err.Error() != "validation error: projects array must not be empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationMissingVersion(t *testing.T) {
	manifest := &EnvManifest{
		Version: "",
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for missing version, got nil")
	}
	if err.Error() != "validation error: version is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationEmptyRequirements(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0.0",
	}
	manifest.Requirements = nil

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for empty requirements, got nil")
	}
	if err.Error() != "validation error: requirements array must not be empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMalformedConfigFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "malformed-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString("{invalid json}"); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpfile.Close()

	_, err = LoadSupervisorConfig(tmpfile.Name())
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
	if err.Error() != "failed to parse config: invalid character 'i' looking for beginning of object key string" {
		t.Logf("got error: %v", err)
	}
}
