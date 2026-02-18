package supervisor

import (
	"context"
	"strings"
	"testing"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// TestManualGuidanceEvent verifies that unsupported Claude remote OAuth
// generates a structured guidance event with correct steps and login command.
func TestManualGuidanceEvent(t *testing.T) {
	// Claude Code does not support remote OAuth (RemoteOAuth=false)
	tool := shared.ToolIdentifier("claude_code")
	nodeID := "agent-node-1"

	guidance := GenerateManualGuidance(tool, nodeID)

	// Verify basic fields
	if guidance.Tool != "claude_code" {
		t.Errorf("expected tool=claude_code, got %q", guidance.Tool)
	}
	if guidance.ReasonCode != "no_remote_oauth" {
		t.Errorf("expected reason_code=no_remote_oauth, got %q", guidance.ReasonCode)
	}
	if guidance.NodeID != nodeID {
		t.Errorf("expected node_id=%q, got %q", nodeID, guidance.NodeID)
	}

	// Verify login command is extracted correctly
	expectedLoginCmd := "claude auth login"
	if guidance.LoginCommand != expectedLoginCmd {
		t.Errorf("expected login_command=%q, got %q", expectedLoginCmd, guidance.LoginCommand)
	}

	// Verify steps are present and ordered
	if len(guidance.Steps) < 3 {
		t.Fatalf("expected at least 3 steps, got %d", len(guidance.Steps))
	}

	// Step 1: SSH instruction
	if !strings.Contains(guidance.Steps[0], "SSH into the agent server") {
		t.Errorf("step 1 should mention SSH, got: %q", guidance.Steps[0])
	}
	if !strings.Contains(guidance.Steps[0], nodeID) {
		t.Errorf("step 1 should include node ID %q, got: %q", nodeID, guidance.Steps[0])
	}

	// Step 2: Login command
	if !strings.Contains(guidance.Steps[1], "Run the login command") {
		t.Errorf("step 2 should mention login command, got: %q", guidance.Steps[1])
	}
	if !strings.Contains(guidance.Steps[1], expectedLoginCmd) {
		t.Errorf("step 2 should include %q, got: %q", expectedLoginCmd, guidance.Steps[1])
	}

	// Step 3: Browser flow
	if !strings.Contains(guidance.Steps[2], "browser-based authentication") {
		t.Errorf("step 3 should mention browser auth, got: %q", guidance.Steps[2])
	}

	// Step 4: Verify status (should exist for claude_code)
	if len(guidance.Steps) < 4 {
		t.Errorf("expected status verification step, got only %d steps", len(guidance.Steps))
	} else {
		if !strings.Contains(guidance.Steps[3], "Verify auth status") {
			t.Errorf("step 4 should mention status verification, got: %q", guidance.Steps[3])
		}
		if !strings.Contains(guidance.Steps[3], "claude auth status") {
			t.Errorf("step 4 should include status command, got: %q", guidance.Steps[3])
		}
	}
}

// TestManualGuidanceMissingNode verifies that missing node context
// generates safe fallback guidance with generic SSH target.
func TestManualGuidanceMissingNode(t *testing.T) {
	tool := shared.ToolIdentifier("claude_code")
	nodeID := "" // Empty node ID

	guidance := GenerateManualGuidance(tool, nodeID)

	// Verify basic fields
	if guidance.Tool != "claude_code" {
		t.Errorf("expected tool=claude_code, got %q", guidance.Tool)
	}
	if guidance.NodeID != "" {
		t.Errorf("expected empty node_id, got %q", guidance.NodeID)
	}

	// Verify SSH step uses generic target
	if len(guidance.Steps) < 1 {
		t.Fatalf("expected at least 1 step, got %d", len(guidance.Steps))
	}

	sshStep := guidance.Steps[0]
	if !strings.Contains(sshStep, "the target agent server") {
		t.Errorf("expected generic SSH target, got: %q", sshStep)
	}
	if strings.Contains(sshStep, "user@") {
		t.Errorf("should not include user@ prefix when node ID is empty, got: %q", sshStep)
	}
}

// TestManualGuidanceAllTools verifies guidance generation for all 3 tools.
func TestManualGuidanceAllTools(t *testing.T) {
	tests := []struct {
		tool               shared.ToolIdentifier
		expectedReasonCode string
		expectedLoginCmd   string
		supportsRemote     bool
	}{
		{
			tool:               "opencode",
			expectedReasonCode: "no_remote_oauth", // opencode supports device code, but this tests the generation logic
			expectedLoginCmd:   "opencode auth login",
			supportsRemote:     true,
		},
		{
			tool:               "claude_code",
			expectedReasonCode: "no_remote_oauth",
			expectedLoginCmd:   "claude auth login",
			supportsRemote:     false,
		},
		{
			tool:               "codex",
			expectedReasonCode: "no_remote_oauth",
			expectedLoginCmd:   "codex login --device-auth",
			supportsRemote:     true,
		},
	}

	nodeID := "test-node"

	for _, tt := range tests {
		t.Run(string(tt.tool), func(t *testing.T) {
			guidance := GenerateManualGuidance(tt.tool, nodeID)

			if guidance.Tool != string(tt.tool) {
				t.Errorf("expected tool=%q, got %q", tt.tool, guidance.Tool)
			}

			if guidance.LoginCommand != tt.expectedLoginCmd {
				t.Errorf("expected login_command=%q, got %q", tt.expectedLoginCmd, guidance.LoginCommand)
			}

			if len(guidance.Steps) < 3 {
				t.Errorf("expected at least 3 steps, got %d", len(guidance.Steps))
			}

			// Verify SSH step includes node ID
			if !strings.Contains(guidance.Steps[0], nodeID) {
				t.Errorf("SSH step should include node ID, got: %q", guidance.Steps[0])
			}

			// Verify login command step
			if !strings.Contains(guidance.Steps[1], guidance.LoginCommand) {
				t.Errorf("login step should include command %q, got: %q", guidance.LoginCommand, guidance.Steps[1])
			}
		})
	}
}

// TestManualGuidanceUnsupportedTool verifies guidance for unknown tool.
func TestManualGuidanceUnsupportedTool(t *testing.T) {
	tool := shared.ToolIdentifier("unknown_tool")
	nodeID := "test-node"

	guidance := GenerateManualGuidance(tool, nodeID)

	if guidance.Tool != "unknown_tool" {
		t.Errorf("expected tool=unknown_tool, got %q", guidance.Tool)
	}

	if guidance.ReasonCode != "provider_unsupported" {
		t.Errorf("expected reason_code=provider_unsupported, got %q", guidance.ReasonCode)
	}

	// Should still generate basic steps
	if len(guidance.Steps) < 3 {
		t.Errorf("expected at least 3 steps even for unknown tool, got %d", len(guidance.Steps))
	}
}

// TestOAuthOrchestratorManualGuidance verifies that OAuthOrchestrator
// includes guidance in the result when manual_required is returned.
func TestOAuthOrchestratorManualGuidance(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	// Mock transport that should NOT be called for unsupported tools
	transport := &mockCommandTransport{}
	dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	orchestrator := NewOAuthOrchestrator(dispatcher, logger)

	ctx := context.Background()
	nodeID := "agent-node-1"
	tool := shared.ToolIdentifier("claude_code")

	result, err := orchestrator.TriggerOAuth(ctx, nodeID, tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "manual_required" {
		t.Errorf("expected status=manual_required, got %q", result.Status)
	}

	if result.ManualGuidance == nil {
		t.Fatal("expected ManualGuidance to be present, got nil")
	}

	guidance := result.ManualGuidance

	if guidance.Tool != "claude_code" {
		t.Errorf("expected tool=claude_code, got %q", guidance.Tool)
	}

	if guidance.ReasonCode != "no_remote_oauth" {
		t.Errorf("expected reason_code=no_remote_oauth, got %q", guidance.ReasonCode)
	}

	if guidance.NodeID != nodeID {
		t.Errorf("expected node_id=%q, got %q", nodeID, guidance.NodeID)
	}

	if guidance.LoginCommand != "claude auth login" {
		t.Errorf("expected login_command='claude auth login', got %q", guidance.LoginCommand)
	}

	if len(guidance.Steps) < 3 {
		t.Errorf("expected at least 3 steps, got %d", len(guidance.Steps))
	}

	// Verify transport was NOT called (no dispatch for unsupported tools)
	if transport.CallCount() != 0 {
		t.Errorf("expected no dispatch for unsupported tool, got %d calls", transport.CallCount())
	}
}
