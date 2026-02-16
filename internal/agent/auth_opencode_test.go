package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
)

// --- Fixtures ---

const fixtureOpencodeAuthenticated = `Stored Credentials:
  Provider: anthropic
    Type: api
    Status: active
    Created: 2026-01-15T10:30:00Z

  Provider: openai
    Type: oauth
    Status: active
    Created: 2026-01-10T08:00:00Z

Environment Variables:
  ANTHROPIC_API_KEY: set (from environment)
`

const fixtureOpencodeAuthenticatedEnvOnly = `Stored Credentials:
  No stored credentials found.

Environment Variables:
  ANTHROPIC_API_KEY: set (from environment)
`

const fixtureOpencodeUnauthenticated = `Stored Credentials:
  No stored credentials found.

Environment Variables:
  No environment variables providing authentication detected.
`

const fixtureOpencodeUnauthenticatedExplicit = `Not authenticated. Run 'opencode auth login' to authenticate.
No credentials found.
`

const fixtureOpencodeUnrecognized = `Some completely unexpected output format
that does not match any known pattern
version 99.99.99
`

// --- Tests ---

func TestOpencodeAuthParserAuthenticated(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		reason  string
	}{
		{
			name:    "stored credentials active",
			fixture: fixtureOpencodeAuthenticated,
			reason:  "credentials found",
		},
		{
			name:    "env var only",
			fixture: fixtureOpencodeAuthenticatedEnvOnly,
			reason:  "credentials found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &MockAuthRunner{
				Result: AuthRunResult{
					Stdout:   tt.fixture,
					ExitCode: 0,
				},
			}
			adapter := NewOpencodeAuthAdapter(runner, nil)
			report := adapter.CheckAuth(context.Background())

			if report.Tool != shared.ToolIdentifierOpenCode {
				t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierOpenCode)
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

func TestOpencodeAuthParserNotInstalled(t *testing.T) {
	tests := []struct {
		name   string
		result AuthRunResult
	}{
		{
			name: "exec error not found",
			result: AuthRunResult{
				Err:      errors.New("exec: \"opencode\": executable file not found in $PATH"),
				ExitCode: -1,
			},
		},
		{
			name: "stderr command not found",
			result: AuthRunResult{
				Stderr:   "bash: opencode: command not found",
				Err:      errors.New("exit status 127"),
				ExitCode: 127,
			},
		},
		{
			name: "no such file",
			result: AuthRunResult{
				Err:      errors.New("fork/exec /usr/local/bin/opencode: no such file or directory"),
				ExitCode: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &MockAuthRunner{Result: tt.result}
			adapter := NewOpencodeAuthAdapter(runner, nil)
			report := adapter.CheckAuth(context.Background())

			if report.Status != shared.AuthStatusNotInstalled {
				t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusNotInstalled)
			}
			if report.Tool != shared.ToolIdentifierOpenCode {
				t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierOpenCode)
			}
		})
	}
}

func TestOpencodeAuthParserUnauthenticated(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
	}{
		{
			name:    "no credentials listed",
			fixture: fixtureOpencodeUnauthenticated,
		},
		{
			name:    "explicit not authenticated message",
			fixture: fixtureOpencodeUnauthenticatedExplicit,
		},
		{
			name:    "empty output",
			fixture: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &MockAuthRunner{
				Result: AuthRunResult{
					Stdout:   tt.fixture,
					ExitCode: 0,
				},
			}
			adapter := NewOpencodeAuthAdapter(runner, nil)
			report := adapter.CheckAuth(context.Background())

			if report.Status != shared.AuthStatusUnauthenticated {
				t.Errorf("Status = %q, want %q\nFixture:\n%s", report.Status, shared.AuthStatusUnauthenticated, tt.fixture)
			}
			if report.Tool != shared.ToolIdentifierOpenCode {
				t.Errorf("Tool = %q, want %q", report.Tool, shared.ToolIdentifierOpenCode)
			}
		})
	}
}

func TestOpencodeAuthParserTimeout(t *testing.T) {
	runner := &MockAuthRunner{
		Result: AuthRunResult{
			TimedOut: true,
			Err:      context.DeadlineExceeded,
			ExitCode: -1,
		},
	}
	adapter := NewOpencodeAuthAdapter(runner, nil)
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusError {
		t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusError)
	}
	if report.Reason != "command timed out" {
		t.Errorf("Reason = %q, want %q", report.Reason, "command timed out")
	}
}

func TestOpencodeAuthParserUnrecognizedOutput(t *testing.T) {
	runner := &MockAuthRunner{
		Result: AuthRunResult{
			Stdout:   fixtureOpencodeUnrecognized,
			ExitCode: 0,
		},
	}
	adapter := NewOpencodeAuthAdapter(runner, nil)
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusError {
		t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusError)
	}
	if report.Reason != "unexpected output format" {
		t.Errorf("Reason = %q, want %q", report.Reason, "unexpected output format")
	}
}

func TestOpencodeAuthParserNonZeroExit(t *testing.T) {
	runner := &MockAuthRunner{
		Result: AuthRunResult{
			Stderr:   "error: database locked",
			Err:      errors.New("exit status 1"),
			ExitCode: 1,
		},
	}
	adapter := NewOpencodeAuthAdapter(runner, nil)
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusError {
		t.Errorf("Status = %q, want %q", report.Status, shared.AuthStatusError)
	}
	if report.Reason == "" {
		t.Error("Reason should not be empty for error status")
	}
}

func TestOpencodeAuthParserEnvVarNotSet(t *testing.T) {
	// Env var listed but marked as "not set" should NOT count as authenticated
	fixture := `Stored Credentials:
  No stored credentials found.

Environment Variables:
  ANTHROPIC_API_KEY: not set
`
	runner := &MockAuthRunner{
		Result: AuthRunResult{
			Stdout:   fixture,
			ExitCode: 0,
		},
	}
	adapter := NewOpencodeAuthAdapter(runner, nil)
	report := adapter.CheckAuth(context.Background())

	// Should not be authenticated — env var is listed but "not set"
	if report.Status == shared.AuthStatusAuthenticated {
		t.Errorf("Status = %q, should NOT be authenticated when env var is 'not set'", report.Status)
	}
}
