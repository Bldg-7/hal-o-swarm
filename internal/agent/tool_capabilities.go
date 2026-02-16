package agent

// ToolID represents a unique identifier for a supported LLM tool.
type ToolID string

const (
	ToolOpencode   ToolID = "opencode"
	ToolClaudeCode ToolID = "claude_code"
	ToolCodex      ToolID = "codex"
)

// ToolCapability encodes authentication status commands, remote OAuth support,
// and manual-only fallback for a supported LLM tool.
type ToolCapability struct {
	// ID is the unique identifier for this tool.
	ID ToolID

	// Name is the human-readable name of the tool.
	Name string

	// StatusCommand is the command to check authentication status.
	// Example: ["opencode", "auth", "list"]
	StatusCommand []string

	// StatusParser indicates how to parse the status command output.
	// Valid values: "output_parse" (parse command output) or "exit_code" (check exit code).
	StatusParser string

	// RemoteOAuth indicates if the tool supports headless/device-code OAuth flow.
	RemoteOAuth bool

	// OAuthFlows lists supported remote OAuth flows.
	// Example: ["device_code"] or empty if RemoteOAuth is false.
	OAuthFlows []string

	// ManualLoginHint provides SSH instructions for manual authentication.
	ManualLoginHint string
}

// GetToolCapabilities returns a map of all supported tool capabilities.
func GetToolCapabilities() map[ToolID]ToolCapability {
	return map[ToolID]ToolCapability{
		ToolOpencode: {
			ID:              ToolOpencode,
			Name:            "opencode",
			StatusCommand:   []string{"opencode", "auth", "list"},
			StatusParser:    "output_parse",
			RemoteOAuth:     true,
			OAuthFlows:      []string{"device_code"},
			ManualLoginHint: "SSH into agent and run: opencode auth login",
		},
		ToolClaudeCode: {
			ID:              ToolClaudeCode,
			Name:            "claude_code",
			StatusCommand:   []string{"claude", "auth", "status"},
			StatusParser:    "exit_code",
			RemoteOAuth:     false,
			OAuthFlows:      []string{},
			ManualLoginHint: "SSH into agent and run: claude auth login",
		},
		ToolCodex: {
			ID:              ToolCodex,
			Name:            "codex",
			StatusCommand:   []string{"codex", "login", "--status"},
			StatusParser:    "exit_code",
			RemoteOAuth:     true,
			OAuthFlows:      []string{"device_code"},
			ManualLoginHint: "SSH into agent and run: codex login --device-auth",
		},
	}
}

// GetToolCapability returns the capability for a specific tool ID, or nil if not found.
func GetToolCapability(id ToolID) *ToolCapability {
	caps := GetToolCapabilities()
	if cap, ok := caps[id]; ok {
		return &cap
	}
	return nil
}
