package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
)

type mockRunner struct {
	responses map[string]string
	errors    map[string]error
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
}

func (m *mockRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("command not found: %s", name)
}

func TestEnvCheckAllPresent(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, filepath.Join(tmpDir, "AGENT.md"), "# Agent\nmodel: claude\n")
	writeFile(t, filepath.Join(tmpDir, "README.md"), "# Project\n")
	writeFile(t, filepath.Join(tmpDir, "CONTRIBUTING.md"), "# Contributing\n")
	writeFile(t, filepath.Join(tmpDir, ".env.example"), "KEY=value\n")
	mkDir(t, filepath.Join(tmpDir, "src"))
	mkDir(t, filepath.Join(tmpDir, ".git", "hooks"))
	writeExecutable(t, filepath.Join(tmpDir, ".git", "hooks", "pre-commit"), "#!/bin/sh\n")

	runner := newMockRunner()
	runner.responses["node --version"] = "v20.11.0"
	runner.responses["python --version"] = "Python 3.11.4"
	runner.responses["git --version"] = "git version 2.42.0"
	runner.responses["docker --version"] = "Docker version 24.0.7, build afdd53b"
	runner.responses["git config"] = "true"

	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")

	checker := NewEnvChecker(tmpDir, runner)
	reqs := &config.ManifestRequirements{
		Runtime: map[string]string{
			"node":   ">=18.0.0",
			"python": ">=3.10.0",
		},
		Tools: map[string]string{
			"git":    ">=2.30.0",
			"docker": ">=20.0.0",
		},
		EnvVars: map[string]string{
			"ANTHROPIC_API_KEY": "required",
			"OPENAI_API_KEY":    "optional",
		},
		AgentConfig: &config.AgentConfigRequirements{
			Model:       "claude-sonnet-4-20250514",
			Temperature: 0.7,
		},
		Context: &config.ContextRequirements{
			Files:       []string{".env.example"},
			Directories: []string{"src"},
		},
		Git: &config.GitRequirements{
			Hooks:  []string{"pre-commit"},
			Config: map[string]string{"core.autocrlf": "true"},
		},
		Docs: &config.DocsRequirements{
			Required:    []string{"README.md"},
			Recommended: []string{"CONTRIBUTING.md"},
		},
	}

	result := checker.Check(context.Background(), reqs)

	if result.Status != StatusReady {
		t.Errorf("expected status %q, got %q", StatusReady, result.Status)
		for _, d := range result.Drift {
			if d.Status != DriftPass {
				t.Logf("  drift: %s/%s expected=%s actual=%s status=%s", d.Category, d.Item, d.Expected, d.Actual, d.Status)
			}
		}
	}

	if result.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}

	for _, d := range result.Drift {
		if d.Status != DriftPass {
			t.Errorf("expected pass for %s/%s, got %s (expected=%s, actual=%s)", d.Category, d.Item, d.Status, d.Expected, d.Actual)
		}
	}
}

func TestEnvCheckMissing(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, filepath.Join(tmpDir, "CONTRIBUTING.md"), "# Contributing\n")

	runner := newMockRunner()
	runner.responses["node --version"] = "v16.14.0"
	runner.errors["python --version"] = fmt.Errorf("command not found: python")
	runner.responses["git --version"] = "git version 2.42.0"
	runner.errors["git config"] = fmt.Errorf("key not found")

	t.Setenv("OPTIONAL_KEY", "value")

	checker := NewEnvChecker(tmpDir, runner)
	reqs := &config.ManifestRequirements{
		Runtime: map[string]string{
			"node":   ">=18.0.0",
			"python": ">=3.10.0",
		},
		Tools: map[string]string{
			"git": ">=2.30.0",
		},
		EnvVars: map[string]string{
			"ANTHROPIC_API_KEY":              "required",
			"HAL_O_SWARM_OPTIONAL_TEST_KEY_": "optional",
		},
		AgentConfig: &config.AgentConfigRequirements{
			Model:       "claude-sonnet-4-20250514",
			Temperature: 0.7,
		},
		Context: &config.ContextRequirements{
			Files:       []string{".env.example"},
			Directories: []string{"src"},
		},
		Git: &config.GitRequirements{
			Hooks:  []string{"pre-commit"},
			Config: map[string]string{"core.autocrlf": "true"},
		},
		Docs: &config.DocsRequirements{
			Required:    []string{"README.md"},
			Recommended: []string{"CONTRIBUTING.md"},
		},
	}

	result := checker.Check(context.Background(), reqs)

	if result.Status != StatusMissing {
		t.Errorf("expected status %q, got %q", StatusMissing, result.Status)
	}

	failures := map[string]DriftItem{}
	warns := map[string]DriftItem{}
	for _, d := range result.Drift {
		key := d.Category + "/" + d.Item
		if d.Status == DriftFail {
			failures[key] = d
		}
		if d.Status == DriftWarn {
			warns[key] = d
		}
	}

	assertDrift(t, failures, "runtime/node", ">=18.0.0", "16.14.0")

	if _, ok := failures["runtime/python"]; !ok {
		t.Error("expected failure for runtime/python (not found)")
	}

	assertDrift(t, failures, "env_vars/ANTHROPIC_API_KEY", "required", "")
	assertDrift(t, warns, "env_vars/HAL_O_SWARM_OPTIONAL_TEST_KEY_", "optional", "")
	assertDrift(t, failures, "agent_config/AGENT.md", "exists", "missing")
	assertDrift(t, failures, "context/.env.example", "exists", "missing")
	assertDrift(t, failures, "context/src", "directory", "missing")
	assertDrift(t, failures, "git/hook:pre-commit", "installed", "missing")
	assertDrift(t, failures, "git/config:core.autocrlf", "true", "")
	assertDrift(t, failures, "docs/README.md", "exists", "missing")

	if _, ok := failures["docs/CONTRIBUTING.md"]; ok {
		t.Error("CONTRIBUTING.md should not be a failure (it exists)")
	}
}

func TestEnvCheckNilRequirements(t *testing.T) {
	checker := NewEnvChecker(t.TempDir(), newMockRunner())
	result := checker.Check(context.Background(), nil)

	if result.Status != StatusReady {
		t.Errorf("nil requirements should return ready, got %q", result.Status)
	}
	if len(result.Drift) != 0 {
		t.Errorf("nil requirements should have no drift items, got %d", len(result.Drift))
	}
}

func TestEnvCheckDegradedStatus(t *testing.T) {
	tmpDir := t.TempDir()

	checker := NewEnvChecker(tmpDir, newMockRunner())
	reqs := &config.ManifestRequirements{
		EnvVars: map[string]string{
			"HAL_O_SWARM_DEGRADED_TEST_KEY_": "optional",
		},
		Docs: &config.DocsRequirements{
			Recommended: []string{"CHANGELOG.md"},
		},
	}

	result := checker.Check(context.Background(), reqs)

	if result.Status != StatusDegraded {
		t.Errorf("expected status %q, got %q", StatusDegraded, result.Status)
	}

	hasWarn := false
	hasFail := false
	for _, d := range result.Drift {
		if d.Status == DriftWarn {
			hasWarn = true
		}
		if d.Status == DriftFail {
			hasFail = true
		}
	}
	if !hasWarn {
		t.Error("expected at least one warning")
	}
	if hasFail {
		t.Error("degraded status should not have failures")
	}
}

func TestParseVersionString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"v18.14.0", "18.14.0"},
		{"node v18.14.0", "18.14.0"},
		{"Python 3.11.4", "3.11.4"},
		{"git version 2.39.0", "2.39.0"},
		{"Docker version 24.0.7, build afdd53b", "24.0.7"},
		{"java 21.0.1 2023-10-17 LTS", "21.0.1"},
		{"v20.11.0", "20.11.0"},
		{"2.42", "2.42"},
	}

	for _, tt := range tests {
		got := parseVersionString(tt.input)
		if got != tt.expected {
			t.Errorf("parseVersionString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCheckVersionConstraint(t *testing.T) {
	tests := []struct {
		version    string
		constraint string
		want       DriftStatus
	}{
		{"18.14.0", ">=18.0.0", DriftPass},
		{"20.0.0", ">=18.0.0", DriftPass},
		{"16.14.0", ">=18.0.0", DriftFail},
		{"3.11.4", ">=3.10.0", DriftPass},
		{"3.9.0", ">=3.10.0", DriftFail},
		{"2.42.0", ">=2.30.0", DriftPass},
		{"2.42", ">=2.30.0", DriftPass},
		{"invalid", ">=1.0.0", DriftFail},
		{"1.0.0", "invalid-constraint", DriftFail},
	}

	for _, tt := range tests {
		got := checkVersionConstraint(tt.version, tt.constraint)
		if got != tt.want {
			t.Errorf("checkVersionConstraint(%q, %q) = %q, want %q", tt.version, tt.constraint, got, tt.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"18", "18.0.0"},
		{"18.14", "18.14.0"},
		{"18.14.0", "18.14.0"},
		{"v3.11.4", "3.11.4"},
	}

	for _, tt := range tests {
		got := normalizeVersion(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDetermineStatus(t *testing.T) {
	tests := []struct {
		name  string
		items []DriftItem
		want  EnvCheckStatus
	}{
		{"empty", nil, StatusReady},
		{"all pass", []DriftItem{{Status: DriftPass}, {Status: DriftPass}}, StatusReady},
		{"warn only", []DriftItem{{Status: DriftPass}, {Status: DriftWarn}}, StatusDegraded},
		{"fail present", []DriftItem{{Status: DriftPass}, {Status: DriftFail}}, StatusMissing},
		{"fail and warn", []DriftItem{{Status: DriftWarn}, {Status: DriftFail}}, StatusMissing},
	}

	for _, tt := range tests {
		got := determineStatus(tt.items)
		if got != tt.want {
			t.Errorf("determineStatus(%s) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestEnvCheckGitHookNotExecutable(t *testing.T) {
	tmpDir := t.TempDir()
	mkDir(t, filepath.Join(tmpDir, ".git", "hooks"))
	writeFile(t, filepath.Join(tmpDir, ".git", "hooks", "pre-commit"), "#!/bin/sh\n")

	checker := NewEnvChecker(tmpDir, newMockRunner())
	reqs := &config.ManifestRequirements{
		Git: &config.GitRequirements{
			Hooks: []string{"pre-commit"},
		},
	}

	result := checker.Check(context.Background(), reqs)

	found := false
	for _, d := range result.Drift {
		if d.Item == "hook:pre-commit" && d.Status == DriftFail && d.Actual == "not executable" {
			found = true
		}
	}
	if !found {
		t.Error("expected drift item for non-executable hook")
	}
}

func TestEnvCheckContextNotADirectory(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, filepath.Join(tmpDir, "src"), "not a directory")

	checker := NewEnvChecker(tmpDir, newMockRunner())
	reqs := &config.ManifestRequirements{
		Context: &config.ContextRequirements{
			Directories: []string{"src"},
		},
	}

	result := checker.Check(context.Background(), reqs)

	found := false
	for _, d := range result.Drift {
		if d.Item == "src" && d.Status == DriftFail && d.Actual == "not a directory" {
			found = true
		}
	}
	if !found {
		t.Error("expected drift item for path that is not a directory")
	}
}

func TestEnvCheckGitConfigMismatch(t *testing.T) {
	runner := newMockRunner()
	runner.responses["git config"] = "false"

	checker := NewEnvChecker(t.TempDir(), runner)
	reqs := &config.ManifestRequirements{
		Git: &config.GitRequirements{
			Config: map[string]string{"core.autocrlf": "true"},
		},
	}

	result := checker.Check(context.Background(), reqs)

	found := false
	for _, d := range result.Drift {
		if d.Item == "config:core.autocrlf" && d.Status == DriftFail && d.Actual == "false" {
			found = true
		}
	}
	if !found {
		t.Error("expected drift item for config value mismatch")
	}
}

func TestNewEnvCheckerDefaultRunner(t *testing.T) {
	checker := NewEnvChecker("/tmp", nil)
	if checker.runner == nil {
		t.Error("expected default runner to be set")
	}
	if _, ok := checker.runner.(*ExecCommandRunner); !ok {
		t.Error("expected ExecCommandRunner as default")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mkDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func assertDrift(t *testing.T, items map[string]DriftItem, key, expectedExpected, expectedActual string) {
	t.Helper()
	d, ok := items[key]
	if !ok {
		t.Errorf("missing drift item: %s", key)
		return
	}
	if d.Expected != expectedExpected {
		t.Errorf("%s: expected Expected=%q, got %q", key, expectedExpected, d.Expected)
	}
	if d.Actual != expectedActual {
		t.Errorf("%s: expected Actual=%q, got %q", key, expectedActual, d.Actual)
	}
}
