package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/Bldg-7/hal-o-swarm/internal/config"
)

// EnvCheckStatus represents the overall environment check status.
type EnvCheckStatus string

const (
	StatusReady    EnvCheckStatus = "ready"
	StatusDegraded EnvCheckStatus = "degraded"
	StatusMissing  EnvCheckStatus = "missing"
)

// DriftStatus represents the status of a single drift item.
type DriftStatus string

const (
	DriftPass DriftStatus = "pass"
	DriftFail DriftStatus = "fail"
	DriftWarn DriftStatus = "warn"
)

// DriftItem represents a single environment check result.
type DriftItem struct {
	Category string      `json:"category"`
	Item     string      `json:"item"`
	Expected string      `json:"expected"`
	Actual   string      `json:"actual"`
	Status   DriftStatus `json:"status"`
}

// CheckResult represents the overall environment check result.
type CheckResult struct {
	Status    EnvCheckStatus `json:"status"`
	Drift     []DriftItem    `json:"drift"`
	Timestamp time.Time      `json:"timestamp"`
}

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	// Run executes a command and returns combined stdout/stderr output.
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecCommandRunner is the real implementation using os/exec.
type ExecCommandRunner struct{}

// Run executes a command via os/exec.
func (r *ExecCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// EnvChecker performs read-only environment validation against a manifest.
type EnvChecker struct {
	runner  CommandRunner
	baseDir string
}

// NewEnvChecker creates an EnvChecker for a project directory.
func NewEnvChecker(baseDir string, runner CommandRunner) *EnvChecker {
	if runner == nil {
		runner = &ExecCommandRunner{}
	}
	return &EnvChecker{
		runner:  runner,
		baseDir: baseDir,
	}
}

// Check runs all environment checks against the manifest requirements.
// It never mutates the environment (read-only).
func (c *EnvChecker) Check(ctx context.Context, reqs *config.ManifestRequirements) *CheckResult {
	result := &CheckResult{
		Drift:     []DriftItem{},
		Timestamp: time.Now().UTC(),
	}

	if reqs == nil {
		result.Status = StatusReady
		return result
	}

	c.checkRuntime(ctx, reqs.Runtime, result)
	c.checkTools(ctx, reqs.Tools, result)
	c.checkEnvVars(reqs.EnvVars, result)
	c.checkAgentConfig(reqs.AgentConfig, result)
	c.checkContext(reqs.Context, result)
	c.checkGit(ctx, reqs.Git, result)
	c.checkDocs(reqs.Docs, result)

	result.Status = determineStatus(result.Drift)
	return result
}

// checkRuntime verifies runtime versions (node, python, java, etc.) meet semver constraints.
func (c *EnvChecker) checkRuntime(ctx context.Context, runtimes map[string]string, result *CheckResult) {
	for name, constraint := range runtimes {
		actual, err := c.getVersion(ctx, name)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "runtime",
				Item:     name,
				Expected: constraint,
				Actual:   fmt.Sprintf("not found: %v", err),
				Status:   DriftFail,
			})
			continue
		}

		status := checkVersionConstraint(actual, constraint)
		result.Drift = append(result.Drift, DriftItem{
			Category: "runtime",
			Item:     name,
			Expected: constraint,
			Actual:   actual,
			Status:   status,
		})
	}
}

// checkTools verifies tool versions (git, docker, etc.) meet semver constraints.
func (c *EnvChecker) checkTools(ctx context.Context, tools map[string]string, result *CheckResult) {
	for name, constraint := range tools {
		actual, err := c.getVersion(ctx, name)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "tools",
				Item:     name,
				Expected: constraint,
				Actual:   fmt.Sprintf("not found: %v", err),
				Status:   DriftFail,
			})
			continue
		}

		status := checkVersionConstraint(actual, constraint)
		result.Drift = append(result.Drift, DriftItem{
			Category: "tools",
			Item:     name,
			Expected: constraint,
			Actual:   actual,
			Status:   status,
		})
	}
}

// checkEnvVars verifies required/optional environment variables are set.
func (c *EnvChecker) checkEnvVars(envVars map[string]string, result *CheckResult) {
	for name, requirement := range envVars {
		val := os.Getenv(name)
		if val == "" {
			status := DriftFail
			if requirement == "optional" {
				status = DriftWarn
			}
			result.Drift = append(result.Drift, DriftItem{
				Category: "env_vars",
				Item:     name,
				Expected: requirement,
				Actual:   "",
				Status:   status,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "env_vars",
				Item:     name,
				Expected: requirement,
				Actual:   "set",
				Status:   DriftPass,
			})
		}
	}
}

// checkAgentConfig verifies AGENT.md exists in the project directory.
func (c *EnvChecker) checkAgentConfig(agentCfg *config.AgentConfigRequirements, result *CheckResult) {
	if agentCfg == nil {
		return
	}

	agentMDPath := filepath.Join(c.baseDir, "AGENT.md")
	_, err := os.Stat(agentMDPath)
	if err != nil {
		result.Drift = append(result.Drift, DriftItem{
			Category: "agent_config",
			Item:     "AGENT.md",
			Expected: "exists",
			Actual:   "missing",
			Status:   DriftFail,
		})
		return
	}

	result.Drift = append(result.Drift, DriftItem{
		Category: "agent_config",
		Item:     "AGENT.md",
		Expected: "exists",
		Actual:   "exists",
		Status:   DriftPass,
	})
}

// checkContext verifies context files and directories exist.
func (c *EnvChecker) checkContext(ctxReqs *config.ContextRequirements, result *CheckResult) {
	if ctxReqs == nil {
		return
	}

	for _, file := range ctxReqs.Files {
		path := c.resolvePath(file)
		_, err := os.Stat(path)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "context",
				Item:     file,
				Expected: "exists",
				Actual:   "missing",
				Status:   DriftFail,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "context",
				Item:     file,
				Expected: "exists",
				Actual:   "exists",
				Status:   DriftPass,
			})
		}
	}

	for _, dir := range ctxReqs.Directories {
		path := c.resolvePath(dir)
		info, err := os.Stat(path)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "context",
				Item:     dir,
				Expected: "directory",
				Actual:   "missing",
				Status:   DriftFail,
			})
		} else if !info.IsDir() {
			result.Drift = append(result.Drift, DriftItem{
				Category: "context",
				Item:     dir,
				Expected: "directory",
				Actual:   "not a directory",
				Status:   DriftFail,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "context",
				Item:     dir,
				Expected: "directory",
				Actual:   "directory",
				Status:   DriftPass,
			})
		}
	}
}

// checkGit verifies git hooks are installed and git config is set.
func (c *EnvChecker) checkGit(ctx context.Context, gitReqs *config.GitRequirements, result *CheckResult) {
	if gitReqs == nil {
		return
	}

	for _, hook := range gitReqs.Hooks {
		hookPath := filepath.Join(c.baseDir, ".git", "hooks", hook)
		info, err := os.Stat(hookPath)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "git",
				Item:     "hook:" + hook,
				Expected: "installed",
				Actual:   "missing",
				Status:   DriftFail,
			})
		} else if info.Mode()&0111 == 0 {
			result.Drift = append(result.Drift, DriftItem{
				Category: "git",
				Item:     "hook:" + hook,
				Expected: "installed",
				Actual:   "not executable",
				Status:   DriftFail,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "git",
				Item:     "hook:" + hook,
				Expected: "installed",
				Actual:   "installed",
				Status:   DriftPass,
			})
		}
	}

	for key, expected := range gitReqs.Config {
		actual, err := c.runner.Run(ctx, "git", "config", "--get", key)
		if err != nil || actual == "" {
			result.Drift = append(result.Drift, DriftItem{
				Category: "git",
				Item:     "config:" + key,
				Expected: expected,
				Actual:   "",
				Status:   DriftFail,
			})
		} else if actual != expected {
			result.Drift = append(result.Drift, DriftItem{
				Category: "git",
				Item:     "config:" + key,
				Expected: expected,
				Actual:   actual,
				Status:   DriftFail,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "git",
				Item:     "config:" + key,
				Expected: expected,
				Actual:   actual,
				Status:   DriftPass,
			})
		}
	}
}

// checkDocs verifies required and recommended documentation exists.
func (c *EnvChecker) checkDocs(docsReqs *config.DocsRequirements, result *CheckResult) {
	if docsReqs == nil {
		return
	}

	for _, doc := range docsReqs.Required {
		path := c.resolvePath(doc)
		_, err := os.Stat(path)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "docs",
				Item:     doc,
				Expected: "exists",
				Actual:   "missing",
				Status:   DriftFail,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "docs",
				Item:     doc,
				Expected: "exists",
				Actual:   "exists",
				Status:   DriftPass,
			})
		}
	}

	for _, doc := range docsReqs.Recommended {
		path := c.resolvePath(doc)
		_, err := os.Stat(path)
		if err != nil {
			result.Drift = append(result.Drift, DriftItem{
				Category: "docs",
				Item:     doc,
				Expected: "recommended",
				Actual:   "missing",
				Status:   DriftWarn,
			})
		} else {
			result.Drift = append(result.Drift, DriftItem{
				Category: "docs",
				Item:     doc,
				Expected: "recommended",
				Actual:   "exists",
				Status:   DriftPass,
			})
		}
	}
}

// getVersion executes "<name> --version" and parses the version string.
func (c *EnvChecker) getVersion(ctx context.Context, name string) (string, error) {
	out, err := c.runner.Run(ctx, name, "--version")
	if err != nil {
		return "", fmt.Errorf("command %q failed: %w", name+" --version", err)
	}
	return parseVersionString(out), nil
}

// resolvePath resolves a path relative to the base directory.
// Absolute paths are returned as-is.
func (c *EnvChecker) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.baseDir, path)
}

// parseVersionString extracts a semver-compatible version from command output.
// Handles formats like "v18.14.0", "node v18.14.0", "Python 3.11.4",
// "git version 2.39.0", "Docker version 24.0.7, build afdd53b".
func parseVersionString(output string) string {
	re := regexp.MustCompile(`v?(\d+\.\d+(?:\.\d+)?)`)
	match := re.FindStringSubmatch(output)
	if len(match) >= 2 {
		return match[1]
	}
	return strings.TrimSpace(output)
}

// checkVersionConstraint compares a version against a semver constraint.
// Returns DriftPass if satisfied, DriftFail otherwise.
func checkVersionConstraint(version, constraint string) DriftStatus {
	normalized := normalizeVersion(version)
	v, err := semver.NewVersion(normalized)
	if err != nil {
		return DriftFail
	}

	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return DriftFail
	}

	if c.Check(v) {
		return DriftPass
	}
	return DriftFail
}

// normalizeVersion ensures version has 3 parts (X.Y.Z).
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	switch len(parts) {
	case 1:
		return v + ".0.0"
	case 2:
		return v + ".0"
	default:
		return v
	}
}

// determineStatus computes overall status from drift items.
// - ready: no failures or warnings
// - degraded: only warnings (no failures)
// - missing: at least one failure
func determineStatus(items []DriftItem) EnvCheckStatus {
	hasFailure := false
	hasWarning := false

	for _, item := range items {
		switch item.Status {
		case DriftFail:
			hasFailure = true
		case DriftWarn:
			hasWarning = true
		}
	}

	if hasFailure {
		return StatusMissing
	}
	if hasWarning {
		return StatusDegraded
	}
	return StatusReady
}
