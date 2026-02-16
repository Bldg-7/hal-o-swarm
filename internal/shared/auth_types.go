package shared

import "time"

// ToolIdentifier represents the identifier of a tool that requires authentication.
type ToolIdentifier string

const (
	ToolIdentifierOpenCode   ToolIdentifier = "opencode"
	ToolIdentifierClaudeCode ToolIdentifier = "claude_code"
	ToolIdentifierCodex      ToolIdentifier = "codex"
)

// AuthStatus represents the authentication status of a tool.
type AuthStatus string

const (
	AuthStatusAuthenticated   AuthStatus = "authenticated"
	AuthStatusUnauthenticated AuthStatus = "unauthenticated"
	AuthStatusNotInstalled    AuthStatus = "not_installed"
	AuthStatusError           AuthStatus = "error"
	AuthStatusManualRequired  AuthStatus = "manual_required"
)

// AuthStateReport represents the authentication state of a tool on a node.
type AuthStateReport struct {
	Tool      ToolIdentifier `json:"tool"`
	Status    AuthStatus     `json:"status"`
	Reason    string         `json:"reason"`
	CheckedAt time.Time      `json:"checked_at"`
}

// CredentialPushPayload represents the payload for pushing credentials to a target node.
type CredentialPushPayload struct {
	TargetNode string            `json:"target_node"`
	EnvVars    map[string]string `json:"env_vars"`
	Version    int               `json:"version"`
}
