package agent

import (
	"context"
	"strings"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// AuthAdapter is the common interface for tool-specific auth status checkers.
type AuthAdapter interface {
	CheckAuth(ctx context.Context) shared.AuthStateReport
}

// OpencodeAuthAdapter checks opencode authentication status by running
// `opencode auth list` and parsing the output for credential indicators.
type OpencodeAuthAdapter struct {
	runner AuthRunner
	logger *zap.Logger
}

// NewOpencodeAuthAdapter creates an OpencodeAuthAdapter with the given runner and logger.
func NewOpencodeAuthAdapter(runner AuthRunner, logger *zap.Logger) *OpencodeAuthAdapter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &OpencodeAuthAdapter{
		runner: runner,
		logger: logger,
	}
}

// CheckAuth runs `opencode auth list` and maps the output to a canonical AuthStateReport.
func (a *OpencodeAuthAdapter) CheckAuth(ctx context.Context) shared.AuthStateReport {
	cap := GetToolCapability(ToolOpencode)
	if cap == nil {
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    "opencode tool capability not found",
			CheckedAt: time.Now().UTC(),
		}
	}

	result := a.runner.RunAuthCheck(ctx, cap.StatusCommand)

	// Timeout → error
	if result.TimedOut {
		a.logger.Warn("opencode auth check timed out")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    "command timed out",
			CheckedAt: time.Now().UTC(),
		}
	}

	// Command not found → not_installed
	if isCommandNotFound(result) {
		a.logger.Info("opencode not installed")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusNotInstalled,
			Reason:    "opencode command not found",
			CheckedAt: time.Now().UTC(),
		}
	}

	// Parse output for credential indicators
	return a.parseAuthOutput(result)
}

// parseAuthOutput examines stdout/stderr for credential indicators.
func (a *OpencodeAuthAdapter) parseAuthOutput(result AuthRunResult) shared.AuthStateReport {
	output := result.Stdout
	if output == "" {
		output = result.Stderr
	}

	lower := strings.ToLower(output)

	hasAuth := hasAuthenticatedIndicator(lower)
	hasUnauth := hasUnauthenticatedIndicator(lower)

	// Authenticated wins when both present (e.g., no stored creds but env var set)
	if hasAuth {
		a.logger.Debug("opencode authenticated", zap.String("output_preview", truncate(output, 200)))
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusAuthenticated,
			Reason:    "credentials found",
			CheckedAt: time.Now().UTC(),
		}
	}

	if hasUnauth {
		a.logger.Info("opencode unauthenticated", zap.String("output_preview", truncate(output, 200)))
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusUnauthenticated,
			Reason:    "no credentials found",
			CheckedAt: time.Now().UTC(),
		}
	}

	// Empty output with successful exit → unauthenticated (no credentials to list)
	if strings.TrimSpace(output) == "" && result.ExitCode == 0 {
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusUnauthenticated,
			Reason:    "no credentials found",
			CheckedAt: time.Now().UTC(),
		}
	}

	// Non-zero exit with no recognized pattern → error
	if result.ExitCode != 0 {
		reason := "command exited with code " + strings.TrimSpace(strings.Replace(
			result.Stderr, "\n", " ", -1))
		if reason == "command exited with code " {
			reason = "command failed with unknown error"
		}
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    truncate(reason, 200),
			CheckedAt: time.Now().UTC(),
		}
	}

	// Output exists but unrecognized → error (defensive)
	a.logger.Warn("opencode auth output unrecognized", zap.String("output_preview", truncate(output, 200)))
	return shared.AuthStateReport{
		Tool:      shared.ToolIdentifierOpenCode,
		Status:    shared.AuthStatusError,
		Reason:    "unexpected output format",
		CheckedAt: time.Now().UTC(),
	}
}

// isCommandNotFound returns true if the result indicates the command binary was not found.
func isCommandNotFound(result AuthRunResult) bool {
	if result.Err == nil {
		return false
	}
	errMsg := strings.ToLower(result.Err.Error())
	combined := strings.ToLower(result.Stderr + " " + result.Stdout)

	notFoundPatterns := []string{
		"not found",
		"no such file",
		"executable file not found",
		"command not found",
	}

	for _, pattern := range notFoundPatterns {
		if strings.Contains(errMsg, pattern) || strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
}

// hasAuthenticatedIndicator checks for patterns indicating credentials are present.
func hasAuthenticatedIndicator(lower string) bool {
	storedCredentialPatterns := []string{
		"status: active",
		"type: api",
		"type: oauth",
		"type: wellknown",
	}
	for _, p := range storedCredentialPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// Environment variable indicators
	envVarPatterns := []string{
		"anthropic_api_key",
		"openai_api_key",
		"api_key",
	}
	for _, p := range envVarPatterns {
		if strings.Contains(lower, p) {
			// Only count as authenticated if the env var appears to have a value
			// (not just listed as missing/empty)
			if !strings.Contains(lower, "not set") && !strings.Contains(lower, "missing") &&
				!strings.Contains(lower, "empty") && !strings.Contains(lower, "unset") {
				return true
			}
		}
	}

	return false
}

// hasUnauthenticatedIndicator checks for patterns indicating no credentials.
func hasUnauthenticatedIndicator(lower string) bool {
	patterns := []string{
		"not authenticated",
		"no credentials",
		"not logged in",
		"no stored credentials",
		"no providers configured",
		"unauthenticated",
		"login required",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// truncate limits a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func extractLine(s string, idx int) string {
	start := strings.LastIndex(s[:idx], "\n") + 1
	end := strings.Index(s[idx:], "\n")
	if end < 0 {
		return s[start:]
	}
	return s[start : idx+end]
}
