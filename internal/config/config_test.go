package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if manifest.Requirements == nil {
		t.Error("expected requirements to be set")
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
		Version:      "1.0",
		Requirements: nil,
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for empty requirements, got nil")
	}
	if err.Error() != "validation error: requirements is required" {
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

func TestEnvManifestValidationMissingRequirements(t *testing.T) {
	manifest := &EnvManifest{
		Version:      "1.0",
		Requirements: nil,
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for missing requirements, got nil")
	}
	if err.Error() != "validation error: requirements is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationInvalidVersionFormat(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid version format, got nil")
	}
	if !strings.Contains(err.Error(), "version must be in format X.Y") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationInvalidRuntimeVersion(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": "invalid-version"},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid runtime version, got nil")
	}
	if !strings.Contains(err.Error(), "must be valid semver constraint") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationInvalidToolVersion(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			Tools:   map[string]string{"git": "not-a-version"},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid tool version, got nil")
	}
	if !strings.Contains(err.Error(), "must be valid semver constraint") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationInvalidEnvVarStatus(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			EnvVars: map[string]string{"API_KEY": "invalid"},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid env var status, got nil")
	}
	if !strings.Contains(err.Error(), "must be 'required' or 'optional'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationInvalidAgentConfigTemperature(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			AgentConfig: &AgentConfigRequirements{
				Model:       "claude-sonnet-4",
				Temperature: 1.5,
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid temperature, got nil")
	}
	if !strings.Contains(err.Error(), "must be between 0.0 and 1.0") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationMissingAgentConfigModel(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			AgentConfig: &AgentConfigRequirements{
				Model:       "",
				Temperature: 0.7,
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for missing agent config model, got nil")
	}
	if !strings.Contains(err.Error(), "agent_config.model must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationInvalidGitHook(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			Git: &GitRequirements{
				Hooks: []string{"invalid-hook"},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid git hook, got nil")
	}
	if !strings.Contains(err.Error(), "must be a valid git hook name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationValidGitHooks(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			Git: &GitRequirements{
				Hooks: []string{"pre-commit", "commit-msg", "pre-push"},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err != nil {
		t.Errorf("unexpected error for valid git hooks: %v", err)
	}
}

func TestEnvManifestValidationEmptyContextArrays(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			Context: &ContextRequirements{
				Files:       []string{},
				Directories: []string{},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for empty context arrays, got nil")
	}
	if !strings.Contains(err.Error(), "must have at least one file or directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationEmptyDocsArrays(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
			Docs: &DocsRequirements{
				Required:    []string{},
				Recommended: []string{},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for empty docs arrays, got nil")
	}
	if !strings.Contains(err.Error(), "must have at least one required or recommended document") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationProjectOverride(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
		},
		Projects: map[string]*ProjectConfig{
			"my-project": {
				Requirements: &ManifestRequirements{
					Runtime: map[string]string{"node": ">=20.0.0"},
				},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err != nil {
		t.Errorf("unexpected error for valid project override: %v", err)
	}
}

func TestEnvManifestValidationProjectInvalidOverride(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{"node": ">=18.0.0"},
		},
		Projects: map[string]*ProjectConfig{
			"my-project": {
				Requirements: &ManifestRequirements{
					Runtime: map[string]string{"node": "bad-version"},
				},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err == nil {
		t.Error("expected error for invalid project override, got nil")
	}
	if !strings.Contains(err.Error(), "projects.my-project") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnvManifestValidationValidSemverConstraints(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		shouldErr bool
	}{
		{"exact version", "1.2.3", false},
		{"two part version", "1.2", false},
		{"gte constraint", ">=18.0.0", false},
		{"lte constraint", "<=20.0.0", false},
		{"caret constraint", "^1.2.3", false},
		{"tilde constraint", "~2.3.4", false},
		{"gt constraint", ">1.0", false},
		{"lt constraint", "<3.0", false},
		{"invalid format", "invalid", true},
		{"empty string", "", true},
		{"letters in version", "1.2.a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := &EnvManifest{
				Version: "1.0",
				Requirements: &ManifestRequirements{
					Runtime: map[string]string{"node": tt.version},
				},
			}

			err := validateEnvManifest(manifest)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error for version %q, got nil", tt.version)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error for version %q: %v", tt.version, err)
			}
		})
	}
}

func TestEnvManifestValidationCompleteManifest(t *testing.T) {
	manifest := &EnvManifest{
		Version: "1.0",
		Requirements: &ManifestRequirements{
			Runtime: map[string]string{
				"node":   ">=18.0.0",
				"python": ">=3.9",
			},
			Tools: map[string]string{
				"git":    ">=2.30",
				"docker": ">=20.10",
			},
			EnvVars: map[string]string{
				"ANTHROPIC_API_KEY": "required",
				"OPENAI_API_KEY":    "optional",
			},
			AgentConfig: &AgentConfigRequirements{
				Model:       "claude-sonnet-4",
				Temperature: 0.7,
			},
			Context: &ContextRequirements{
				Files:       []string{"README.md", "ARCHITECTURE.md"},
				Directories: []string{"docs/"},
			},
			Git: &GitRequirements{
				Hooks: []string{"pre-commit", "commit-msg"},
				Config: map[string]string{
					"user.name":  "required",
					"user.email": "required",
				},
			},
			Docs: &DocsRequirements{
				Required:    []string{"README.md", "CONTRIBUTING.md"},
				Recommended: []string{"ARCHITECTURE.md"},
			},
		},
		Projects: map[string]*ProjectConfig{
			"my-project": {
				Requirements: &ManifestRequirements{
					Runtime: map[string]string{"node": ">=20.0.0"},
				},
			},
		},
	}

	err := validateEnvManifest(manifest)
	if err != nil {
		t.Errorf("unexpected error for complete valid manifest: %v", err)
	}
}

func TestEnvManifestLoadExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "env-manifest.example.json")
	manifest, err := LoadEnvManifest(examplePath)
	if err != nil {
		t.Fatalf("failed to load example env manifest: %v", err)
	}
	if manifest.Version == "" {
		t.Error("expected version to be set")
	}
	if manifest.Requirements == nil {
		t.Error("expected requirements to be set")
	}
	if len(manifest.Requirements.Runtime) == 0 {
		t.Error("expected runtime requirements to be set")
	}
	if len(manifest.Requirements.Tools) == 0 {
		t.Error("expected tools requirements to be set")
	}
	if len(manifest.Requirements.EnvVars) == 0 {
		t.Error("expected env_vars requirements to be set")
	}
	if manifest.Requirements.AgentConfig == nil {
		t.Error("expected agent_config to be set")
	}
	if manifest.Requirements.Context == nil {
		t.Error("expected context to be set")
	}
	if manifest.Requirements.Git == nil {
		t.Error("expected git to be set")
	}
	if manifest.Requirements.Docs == nil {
		t.Error("expected docs to be set")
	}
	if len(manifest.Projects) == 0 {
		t.Error("expected projects to be set")
	}
}
