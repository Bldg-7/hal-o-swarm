package agent

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAuthRunnerShortCommand(t *testing.T) {
	runner := NewAuthCommandRunner(5*time.Second, zap.NewNop())

	result := runner.RunAuthCheck(context.Background(), []string{"echo", "hello"})

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if result.TimedOut {
		t.Errorf("expected no timeout")
	}
	if result.Stdout != "hello" {
		t.Errorf("expected stdout 'hello', got %q", result.Stdout)
	}
	if result.Stderr != "" {
		t.Errorf("expected empty stderr, got %q", result.Stderr)
	}
}

func TestAuthRunnerTimeout(t *testing.T) {
	runner := NewAuthCommandRunner(100*time.Millisecond, zap.NewNop())

	result := runner.RunAuthCheck(context.Background(), []string{"sleep", "30"})

	if !result.TimedOut {
		t.Errorf("expected timeout")
	}
	if result.Err == nil {
		t.Errorf("expected error on timeout")
	}
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 on timeout, got %d", result.ExitCode)
	}
}

func TestAuthRunnerExitCode(t *testing.T) {
	runner := NewAuthCommandRunner(5*time.Second, zap.NewNop())

	result := runner.RunAuthCheck(context.Background(), []string{"sh", "-c", "exit 42"})

	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
	if result.Err == nil {
		t.Errorf("expected error for non-zero exit code")
	}
	if result.TimedOut {
		t.Errorf("expected no timeout")
	}
}

func TestAuthRunnerStderr(t *testing.T) {
	runner := NewAuthCommandRunner(5*time.Second, zap.NewNop())

	result := runner.RunAuthCheck(context.Background(), []string{"sh", "-c", "echo error >&2"})

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if result.Stderr != "error" {
		t.Errorf("expected stderr 'error', got %q", result.Stderr)
	}
	if result.Stdout != "" {
		t.Errorf("expected empty stdout, got %q", result.Stdout)
	}
}

func TestAuthRunnerEmptyCommand(t *testing.T) {
	runner := NewAuthCommandRunner(5*time.Second, zap.NewNop())

	result := runner.RunAuthCheck(context.Background(), []string{})

	if result.Err == nil {
		t.Errorf("expected error for empty command")
	}
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 for empty command, got %d", result.ExitCode)
	}
}

func TestAuthRunnerDefaultTimeout(t *testing.T) {
	runner := NewAuthCommandRunner(0, zap.NewNop())

	// Verify default timeout is 10 seconds by checking the timeout field
	if runner.timeout != 10*time.Second {
		t.Errorf("expected default timeout 10s, got %v", runner.timeout)
	}
}

func TestMockAuthRunner(t *testing.T) {
	mock := &MockAuthRunner{
		Result: AuthRunResult{
			Stdout:   "mocked output",
			Stderr:   "",
			ExitCode: 0,
			Err:      nil,
			TimedOut: false,
		},
	}

	result := mock.RunAuthCheck(context.Background(), []string{"any", "command"})

	if result.Stdout != "mocked output" {
		t.Errorf("expected mocked stdout, got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}
