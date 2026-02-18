package agent

import (
	"context"
	"strings"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// ClaudeAuthAdapter checks Claude Code authentication status by running
// `claude auth status` and mapping the exit code to auth status.
type ClaudeAuthAdapter struct {
	runner        AuthRunner
	logger        *zap.Logger
	statusCommand []string
}

func NewClaudeAuthAdapter(runner AuthRunner, logger *zap.Logger) *ClaudeAuthAdapter {
	return NewClaudeAuthAdapterWithCommand(runner, logger, nil)
}

func NewClaudeAuthAdapterWithCommand(runner AuthRunner, logger *zap.Logger, statusCommand []string) *ClaudeAuthAdapter {
	if logger == nil {
		logger = zap.NewNop()
	}
	if len(statusCommand) == 0 {
		if cap := GetToolCapability(ToolClaudeCode); cap != nil {
			statusCommand = append([]string(nil), cap.StatusCommand...)
		}
	}

	return &ClaudeAuthAdapter{
		runner:        runner,
		logger:        logger,
		statusCommand: statusCommand,
	}
}

func (a *ClaudeAuthAdapter) CheckAuth(ctx context.Context) shared.AuthStateReport {
	if len(a.statusCommand) == 0 {
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierClaudeCode,
			Status:    shared.AuthStatusError,
			Reason:    "claude status command not configured",
			CheckedAt: time.Now().UTC(),
		}
	}

	result := a.runner.RunAuthCheck(ctx, a.statusCommand)

	if result.TimedOut {
		a.logger.Warn("claude auth check timed out")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierClaudeCode,
			Status:    shared.AuthStatusError,
			Reason:    "command timed out",
			CheckedAt: time.Now().UTC(),
		}
	}

	if isCommandNotFound(result) {
		a.logger.Info("claude not installed")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierClaudeCode,
			Status:    shared.AuthStatusNotInstalled,
			Reason:    "claude command not found",
			CheckedAt: time.Now().UTC(),
		}
	}

	if result.ExitCode == 0 {
		reason := "authenticated via claude auth status"
		if detail := parseClaudeContext(result.Stdout); detail != "" {
			reason = detail
		}
		a.logger.Debug("claude authenticated", zap.String("reason", reason))
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierClaudeCode,
			Status:    shared.AuthStatusAuthenticated,
			Reason:    reason,
			CheckedAt: time.Now().UTC(),
		}
	}

	reason := "not authenticated"
	if detail := parseClaudeContext(result.Stdout + " " + result.Stderr); detail != "" {
		reason = detail
	}
	a.logger.Info("claude unauthenticated", zap.Int("exit_code", result.ExitCode), zap.String("reason", reason))
	return shared.AuthStateReport{
		Tool:      shared.ToolIdentifierClaudeCode,
		Status:    shared.AuthStatusUnauthenticated,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}

func parseClaudeContext(output string) string {
	lower := strings.ToLower(output)
	if idx := strings.Index(lower, "logged in as"); idx >= 0 {
		return truncate(strings.TrimSpace(extractLine(output, idx)), 200)
	}
	if idx := strings.Index(lower, "account:"); idx >= 0 {
		return truncate(strings.TrimSpace(extractLine(output, idx)), 200)
	}
	return ""
}
