package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// AuthAdapter is the common interface for tool-specific auth status checkers.
type AuthAdapter interface {
	CheckAuth(ctx context.Context) shared.AuthStateReport
}

type OpencodeAuthAdapter struct {
	runner        AuthRunner
	logger        *zap.Logger
	statusCommand []string
}

// NewOpencodeAuthAdapter creates an OpencodeAuthAdapter with the given runner and logger.
func NewOpencodeAuthAdapter(runner AuthRunner, logger *zap.Logger) *OpencodeAuthAdapter {
	return NewOpencodeAuthAdapterWithCommand(runner, logger, nil)
}

func NewOpencodeAuthAdapterWithCommand(runner AuthRunner, logger *zap.Logger, statusCommand []string) *OpencodeAuthAdapter {
	if logger == nil {
		logger = zap.NewNop()
	}
	if len(statusCommand) == 0 {
		if cap := GetToolCapability(ToolOpencode); cap != nil {
			statusCommand = append([]string(nil), cap.StatusCommand...)
		}
	}

	return &OpencodeAuthAdapter{
		runner:        runner,
		logger:        logger,
		statusCommand: statusCommand,
	}
}

func (a *OpencodeAuthAdapter) CheckAuth(ctx context.Context) shared.AuthStateReport {
	_ = ctx

	if len(a.statusCommand) == 0 {
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    "opencode status command not configured",
			CheckedAt: time.Now().UTC(),
		}
	}

	if !commandAvailable(a.statusCommand[0]) {
		a.logger.Info("opencode not installed")
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusNotInstalled,
			Reason:    "opencode command not found",
			CheckedAt: time.Now().UTC(),
		}
	}

	authFilePath, err := opencodeAuthFilePath()
	if err != nil {
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    "unable to resolve opencode auth file path",
			CheckedAt: time.Now().UTC(),
		}
	}

	content, err := os.ReadFile(authFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return shared.AuthStateReport{
				Tool:      shared.ToolIdentifierOpenCode,
				Status:    shared.AuthStatusUnauthenticated,
				Reason:    "no credentials found",
				CheckedAt: time.Now().UTC(),
			}
		}

		a.logger.Warn("failed to read opencode auth file",
			zap.String("path", authFilePath),
			zap.Error(err),
		)
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    "failed to read opencode auth file",
			CheckedAt: time.Now().UTC(),
		}
	}

	credentials, err := countCredentialsFromAuthFile(content)
	if err != nil {
		a.logger.Warn("failed to parse opencode auth file",
			zap.String("path", authFilePath),
			zap.Error(err),
		)
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusError,
			Reason:    "invalid auth file format",
			CheckedAt: time.Now().UTC(),
		}
	}

	if credentials > 0 {
		a.logger.Debug("opencode authenticated",
			zap.Int("credentials", credentials),
			zap.String("path", authFilePath),
		)
		return shared.AuthStateReport{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusAuthenticated,
			Reason:    fmt.Sprintf("%d credentials found", credentials),
			CheckedAt: time.Now().UTC(),
		}
	}

	return shared.AuthStateReport{
		Tool:      shared.ToolIdentifierOpenCode,
		Status:    shared.AuthStatusUnauthenticated,
		Reason:    "no credentials found",
		CheckedAt: time.Now().UTC(),
	}
}

func commandAvailable(binary string) bool {
	if strings.TrimSpace(binary) == "" {
		return false
	}

	if filepath.IsAbs(binary) {
		return isExecutableFile(binary)
	}

	_, err := exec.LookPath(binary)
	return err == nil
}

func opencodeAuthFilePath() (string, error) {
	if xdgDataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "opencode", "auth.json"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("resolve user home: %w", err)
	}

	return filepath.Join(homeDir, ".local", "share", "opencode", "auth.json"), nil
}

func countCredentialsFromAuthFile(content []byte) (int, error) {
	if len(strings.TrimSpace(string(content))) == 0 {
		return 0, nil
	}

	var decoded any
	if err := json.Unmarshal(content, &decoded); err != nil {
		return 0, err
	}

	return countCredentialNodes(decoded), nil
}

func countCredentialNodes(value any) int {
	switch v := value.(type) {
	case []any:
		return len(v)
	case map[string]any:
		for _, key := range []string{"credentials", "providers", "accounts", "tokens", "auth"} {
			if nested, ok := v[key]; ok {
				count := countCredentialCollection(nested)
				if count > 0 {
					return count
				}
			}
		}
		return countCredentialCollection(v)
	default:
		return 0
	}
}

func countCredentialCollection(value any) int {
	switch v := value.(type) {
	case []any:
		total := 0
		for _, item := range v {
			total += countCredentialCollection(item)
		}
		if total > 0 {
			return total
		}
		return len(v)
	case map[string]any:
		if looksLikeCredential(v) {
			return 1
		}
		total := 0
		for _, nested := range v {
			total += countCredentialCollection(nested)
		}
		return total
	default:
		return 0
	}
}

func looksLikeCredential(fields map[string]any) bool {
	credentialKeys := []string{
		"provider",
		"type",
		"token",
		"access_token",
		"refresh_token",
		"api_key",
		"kind",
	}

	for _, key := range credentialKeys {
		if value, ok := fields[key]; ok {
			if text, ok := value.(string); ok {
				if strings.TrimSpace(text) != "" {
					return true
				}
				continue
			}

			if value != nil {
				return true
			}
		}
	}

	return false
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
