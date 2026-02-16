package agent

import (
	"context"
	"strings"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// CodexAuthAdapter checks Codex authentication status by running
// `codex login --status` and mapping the exit code to auth status.
type CodexAuthAdapter struct {
	runner AuthRunner
	logger *zap.Logger
}

func NewCodexAuthAdapter(runner AuthRunner, logger *zap.Logger) *CodexAuthAdapter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &CodexAuthAdapter{
		runner: runner,
		logger: logger,
	}
}

func (a *CodexAuthAdapter) CheckAuth(ctx context.Context) shared.AuthStateReport {
	cap := GetToolCapability(ToolCodex)
	if cap == nil {
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierCodex,
			Status:    shared.AuthStatusError,
			Reason:    "codex tool capability not found",
			CheckedAt: time.Now().UTC(),
		}
	}

	result := a.runner.RunAuthCheck(ctx, cap.StatusCommand)

	if result.TimedOut {
		a.logger.Warn("codex auth check timed out")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierCodex,
			Status:    shared.AuthStatusError,
			Reason:    "command timed out",
			CheckedAt: time.Now().UTC(),
		}
	}

	if isCommandNotFound(result) {
		a.logger.Info("codex not installed")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierCodex,
			Status:    shared.AuthStatusNotInstalled,
			Reason:    "codex command not found",
			CheckedAt: time.Now().UTC(),
		}
	}

	if result.ExitCode == 0 {
		reason := "authenticated via codex login --status"
		if detail := parseCodexContext(result.Stdout); detail != "" {
			reason = detail
		}
		a.logger.Debug("codex authenticated", zap.String("reason", reason))
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierCodex,
			Status:    shared.AuthStatusAuthenticated,
			Reason:    reason,
			CheckedAt: time.Now().UTC(),
		}
	}

	reason := "not authenticated"
	if detail := parseCodexContext(result.Stdout + " " + result.Stderr); detail != "" {
		reason = detail
	}
	a.logger.Info("codex unauthenticated", zap.Int("exit_code", result.ExitCode), zap.String("reason", reason))
	return shared.AuthStateReport{
		Tool:      shared.ToolIdentifierCodex,
		Status:    shared.AuthStatusUnauthenticated,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}

func parseCodexContext(output string) string {
	lower := strings.ToLower(output)
	if idx := strings.Index(lower, "logged in as"); idx >= 0 {
		return truncate(strings.TrimSpace(extractLine(output, idx)), 200)
	}
	if idx := strings.Index(lower, "authenticated as"); idx >= 0 {
		return truncate(strings.TrimSpace(extractLine(output, idx)), 200)
	}
	return ""
}
