package agent

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
)

// AuthRunResult represents the result of running an auth check command.
type AuthRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	TimedOut bool
}

// AuthRunner abstracts auth command execution for testability.
type AuthRunner interface {
	RunAuthCheck(ctx context.Context, command []string) AuthRunResult
}

// AuthCommandRunner is the real implementation using os/exec with timeout support.
type AuthCommandRunner struct {
	timeout time.Duration
	logger  *zap.Logger
}

// NewAuthCommandRunner creates an AuthCommandRunner with configurable timeout.
// If timeout is 0, defaults to 10 seconds.
func NewAuthCommandRunner(timeout time.Duration, logger *zap.Logger) *AuthCommandRunner {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AuthCommandRunner{
		timeout: timeout,
		logger:  logger,
	}
}

// RunAuthCheck executes a command with timeout and captures stdout/stderr separately.
func (r *AuthCommandRunner) RunAuthCheck(ctx context.Context, command []string) AuthRunResult {
	if len(command) == 0 {
		return AuthRunResult{
			Err:      errors.New("empty command"),
			ExitCode: -1,
		}
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Create command with timeout context
	cmd := exec.CommandContext(timeoutCtx, command[0], command[1:]...)

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()

	result := AuthRunResult{
		Stdout: trimOutput(stdout.String()),
		Stderr: trimOutput(stderr.String()),
	}

	// Check if context was canceled due to timeout
	if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.Err = timeoutCtx.Err()
		result.ExitCode = -1
		return result
	}

	// Extract exit code from error
	if err != nil {
		result.Err = err
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result
	}

	// Success case
	result.ExitCode = 0
	return result
}

// MockAuthRunner is a test implementation that returns configurable results.
type MockAuthRunner struct {
	Result AuthRunResult
}

// RunAuthCheck returns the configured result.
func (m *MockAuthRunner) RunAuthCheck(ctx context.Context, command []string) AuthRunResult {
	return m.Result
}

// trimOutput removes leading and trailing whitespace from output.
func trimOutput(s string) string {
	return strings.TrimSpace(s)
}
