package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type recordingAuthRunner struct {
	called  bool
	command []string
	result  AuthRunResult
}

func (r *recordingAuthRunner) RunAuthCheck(ctx context.Context, command []string) AuthRunResult {
	r.called = true
	r.command = append([]string(nil), command...)
	return r.result
}

func TestOAuthTriggerExecutorSupported(t *testing.T) {
	runner := &recordingAuthRunner{result: AuthRunResult{Stdout: "Visit https://auth.openai.com/device and enter code: TEST-1234", ExitCode: 0}}
	executor := NewOAuthTriggerExecutor(runner, zap.NewNop())

	result := executor.Trigger(context.Background(), shared.ToolIdentifierCodex)
	if result.Status != "challenge" {
		t.Fatalf("expected challenge status, got %s", result.Status)
	}
	if result.ChallengeURL != "https://auth.openai.com/device" {
		t.Fatalf("unexpected challenge url %s", result.ChallengeURL)
	}
	if result.UserCode != "TEST-1234" {
		t.Fatalf("unexpected user code %s", result.UserCode)
	}
	if !runner.called {
		t.Fatal("expected auth runner to be called")
	}
}

func TestOAuthTriggerExecutorUnsupported(t *testing.T) {
	runner := &recordingAuthRunner{}
	executor := NewOAuthTriggerExecutor(runner, zap.NewNop())

	result := executor.Trigger(context.Background(), shared.ToolIdentifierClaudeCode)
	if result.Status != "manual_required" {
		t.Fatalf("expected manual_required status, got %s", result.Status)
	}
	if runner.called {
		t.Fatal("expected auth runner not to be called")
	}
}

func TestExecuteOAuthTriggerCommand(t *testing.T) {
	executor := NewOAuthTriggerExecutor(nil, zap.NewNop())

	payload, err := json.Marshal(map[string]interface{}{
		"type": "oauth_trigger",
		"args": map[string]interface{}{
			"tool": "opencode",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	result, err := ExecuteOAuthTriggerCommand(context.Background(), &shared.Envelope{Payload: payload}, executor)
	if err != nil {
		t.Fatalf("execute oauth trigger command failed: %v", err)
	}
	if result.Status != "challenge" {
		t.Fatalf("expected challenge status, got %s", result.Status)
	}
	if result.ChallengeURL == "" || result.UserCode == "" {
		t.Fatal("expected challenge url and user code in stub response")
	}
}
