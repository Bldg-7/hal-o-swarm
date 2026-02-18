package supervisor

import (
	"fmt"

	"github.com/Bldg-7/hal-o-swarm/internal/agent"
	"github.com/Bldg-7/hal-o-swarm/internal/shared"
)

// ManualAuthGuidance provides structured guidance for manual authentication
// when remote OAuth is not supported for a tool.
type ManualAuthGuidance struct {
	Tool         string   `json:"tool"`
	ReasonCode   string   `json:"reason_code"`   // "no_remote_oauth", "provider_unsupported"
	Reason       string   `json:"reason"`        // Human-readable
	Steps        []string `json:"steps"`         // Ordered steps for operator
	LoginCommand string   `json:"login_command"` // e.g., "claude auth login"
	NodeID       string   `json:"node_id"`
}

// GenerateManualGuidance creates structured manual authentication guidance
// for a tool that doesn't support remote OAuth.
func GenerateManualGuidance(tool shared.ToolIdentifier, nodeID string) ManualAuthGuidance {
	capability := agent.GetToolCapability(agent.ToolID(tool))

	guidance := ManualAuthGuidance{
		Tool:   string(tool),
		NodeID: nodeID,
	}

	// Determine reason code and reason
	if capability == nil {
		guidance.ReasonCode = "provider_unsupported"
		guidance.Reason = fmt.Sprintf("Tool %q is not supported for remote OAuth", tool)
		guidance.LoginCommand = fmt.Sprintf("%s auth login", tool)
	} else if !capability.RemoteOAuth {
		guidance.ReasonCode = "no_remote_oauth"
		guidance.Reason = fmt.Sprintf("Tool %q does not support remote OAuth flows", capability.Name)
		guidance.LoginCommand = extractLoginCommand(capability.ManualLoginHint)
	} else {
		guidance.ReasonCode = "no_remote_oauth"
		guidance.Reason = fmt.Sprintf("Tool %q requires manual authentication", tool)
		guidance.LoginCommand = extractLoginCommand(capability.ManualLoginHint)
	}

	// Generate steps
	sshTarget := nodeID
	if sshTarget == "" {
		sshTarget = "the target agent server"
	} else {
		sshTarget = fmt.Sprintf("user@%s", sshTarget)
	}

	guidance.Steps = []string{
		fmt.Sprintf("SSH into the agent server: ssh %s", sshTarget),
		fmt.Sprintf("Run the login command: %s", guidance.LoginCommand),
		"Follow the browser-based authentication flow",
	}

	// Add status verification step if capability exists
	if capability != nil && len(capability.StatusCommand) > 0 {
		statusCmd := joinCommand(capability.StatusCommand)
		guidance.Steps = append(guidance.Steps, fmt.Sprintf("Verify auth status: %s", statusCmd))
	}

	return guidance
}

// extractLoginCommand extracts the command from a manual login hint.
// Example: "SSH into agent and run: claude auth login" -> "claude auth login"
func extractLoginCommand(hint string) string {
	if hint == "" {
		return ""
	}

	const runPrefix = "run: "
	const runPrefixNoSpace = "run:"

	if idx := findSubstring(hint, runPrefix); idx >= 0 {
		return hint[idx+len(runPrefix):]
	}
	if idx := findSubstring(hint, runPrefixNoSpace); idx >= 0 {
		return hint[idx+len(runPrefixNoSpace):]
	}

	return hint
}

// findSubstring returns the index of substr in s, or -1 if not found.
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// joinCommand joins command parts with spaces.
func joinCommand(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += " " + parts[i]
	}
	return result
}
