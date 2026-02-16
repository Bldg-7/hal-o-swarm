package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
)

func TestCodexAuthAdapterAuthenticated(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		reason string
	}{
		{
			name:   "exit 0 minimal",
			stdout: "",
			reason: "authenticated via codex login --status",
		},
		{
			name:   "exit 0 with logged in context",
			stdout: "Logged in as user@example.com",
			reason: "Logged in as user@example.com",
		},
		{
			name:   "exit 0 with authenticated as context",
			stdout: "Authenticated as org/user\nAPI key source: keyring",
			reason: "Authenticated as org/user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &MockAuthRunner{
				Result: AuthRunResult{
					Stdout:   tt.stdout,
					ExitCode: 0,
				},
			}
			adapter := NewCodexAuthAdapter(runner, nil)
			report := adapter.CheckAuth(context.Background())

			if report.Tool != shared.ToolIdentifierCodex {
				t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierCodex)
			}
			if report.Status != shared.AuthStatusAuthenticated {
				t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusAuthenticated)
			}
			if report.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", report.Reason, tt.reason)
			}
			if report.CheckedAt.IsZero() {
				t.Error("CheckedAt should not be zero")
			}
		})
	}
}

func TestCodexAuthAdapterUnauthenticated(t *testing.T) {
	tests := []struct {
		name   string
		result AuthRunResult
		reason string
	}{
		{
			name: "exit 1 no output",
			result: AuthRunResult{
				ExitCode: 1,
				Err:      errors.New("exit status 1"),
			},
			reason: "not authenticated",
		},
		{
			name: "exit 1 with stderr",
			result: AuthRunResult{
				ExitCode: 1,
				Stderr:   "Not logged in. Run 'codex login' to authenticate.",
				Err:      errors.New("exit status 1"),
			},
			reason: "not authenticated",
		},
		{
			name: "exit 2",
			result: AuthRunResult{
				ExitCode: 2,
				Err:      errors.New("exit status 2"),
			},
			reason: "not authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &MockAuthRunner{Result: tt.result}
			adapter := NewCodexAuthAdapter(runner, nil)
			report := adapter.CheckAuth(context.Background())

			if report.Status != shared.AuthStatusUnauthenticated {
				t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusUnauthenticated)
			}
			if report.Tool != shared.ToolIdentifierCodex {
				t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierCodex)
			}
			if report.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", report.Reason, tt.reason)
			}
		})
	}
}

func TestCodexAuthAdapterNotInstalled(t *testing.T) {
	tests := []struct {
		name   string
		result AuthRunResult
	}{
		{
			name: "executable file not found",
			result: AuthRunResult{
				Err:      errors.New("exec: \"codex\": executable file not found in $PATH"),
				ExitCode: -1,
			},
		},
		{
			name: "command not found via stderr",
			result: AuthRunResult{
				Stderr:   "bash: codex: command not found",
				Err:      errors.New("exit status 127"),
				ExitCode: 127,
			},
		},
		{
			name: "no such file",
			result: AuthRunResult{
				Err:      errors.New("fork/exec /usr/local/bin/codex: no such file or directory"),
				ExitCode: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &MockAuthRunner{Result: tt.result}
			adapter := NewCodexAuthAdapter(runner, nil)
			report := adapter.CheckAuth(context.Background())

			if report.Status != shared.AuthStatusNotInstalled {
				t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusNotInstalled)
			}
			if report.Tool != shared.ToolIdentifierCodex {
				t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierCodex)
			}
		})
	}
}

func TestCodexAuthAdapterTimeout(t *testing.T) {
	runner := &MockAuthRunner{
		Result: AuthRunResult{
			TimedOut: true,
			Err:      context.DeadlineExceeded,
			ExitCode: -1,
		},
	}
	adapter := NewCodexAuthAdapter(runner, nil)
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusError {
		t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusError)
	}
	if report.Reason != "command timed out" {
		t.Errorf("Reason = %q, want %q", report.Reason, "command timed out")
	}
	if report.Tool != shared.ToolIdentifierCodex {
		t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierCodex)
	}
}

func TestCodexAuthAdapterImplementsInterface(t *testing.T) {
	var _ AuthAdapter = (*CodexAuthAdapter)(nil)
}
