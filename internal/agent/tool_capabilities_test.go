package agent

import (
	"testing"
)

// TestToolCapabilityMatrix verifies all 3 tools return correct capabilities.
func TestToolCapabilityMatrix(t *testing.T) {
	tests := []struct {
		name     string
		toolID   ToolID
		expected ToolCapability
	}{
		{
			name:   "opencode capability",
			toolID: ToolOpencode,
			expected: ToolCapability{
				ID:              ToolOpencode,
				Name:            "opencode",
				StatusCommand:   []string{"opencode", "auth", "list"},
				StatusParser:    "output_parse",
				RemoteOAuth:     true,
				OAuthFlows:      []string{"device_code"},
				ManualLoginHint: "SSH into agent and run: opencode auth login",
			},
		},
		{
			name:   "claude_code capability",
			toolID: ToolClaudeCode,
			expected: ToolCapability{
				ID:              ToolClaudeCode,
				Name:            "claude_code",
				StatusCommand:   []string{"claude", "auth", "status"},
				StatusParser:    "exit_code",
				RemoteOAuth:     false,
				OAuthFlows:      []string{},
				ManualLoginHint: "SSH into agent and run: claude auth login",
			},
		},
		{
			name:   "codex capability",
			toolID: ToolCodex,
			expected: ToolCapability{
				ID:              ToolCodex,
				Name:            "codex",
				StatusCommand:   []string{"codex", "login", "--status"},
				StatusParser:    "exit_code",
				RemoteOAuth:     true,
				OAuthFlows:      []string{"device_code"},
				ManualLoginHint: "SSH into agent and run: codex login --device-auth",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cap := GetToolCapability(tt.toolID)
			if cap == nil {
				t.Fatalf("GetToolCapability(%q) returned nil, expected capability", tt.toolID)
			}

			if cap.ID != tt.expected.ID {
				t.Errorf("ID: got %q, want %q", cap.ID, tt.expected.ID)
			}
			if cap.Name != tt.expected.Name {
				t.Errorf("Name: got %q, want %q", cap.Name, tt.expected.Name)
			}
			if len(cap.StatusCommand) != len(tt.expected.StatusCommand) {
				t.Errorf("StatusCommand length: got %d, want %d", len(cap.StatusCommand), len(tt.expected.StatusCommand))
			} else {
				for i, cmd := range cap.StatusCommand {
					if cmd != tt.expected.StatusCommand[i] {
						t.Errorf("StatusCommand[%d]: got %q, want %q", i, cmd, tt.expected.StatusCommand[i])
					}
				}
			}
			if cap.StatusParser != tt.expected.StatusParser {
				t.Errorf("StatusParser: got %q, want %q", cap.StatusParser, tt.expected.StatusParser)
			}
			if cap.RemoteOAuth != tt.expected.RemoteOAuth {
				t.Errorf("RemoteOAuth: got %v, want %v", cap.RemoteOAuth, tt.expected.RemoteOAuth)
			}
			if len(cap.OAuthFlows) != len(tt.expected.OAuthFlows) {
				t.Errorf("OAuthFlows length: got %d, want %d", len(cap.OAuthFlows), len(tt.expected.OAuthFlows))
			} else {
				for i, flow := range cap.OAuthFlows {
					if flow != tt.expected.OAuthFlows[i] {
						t.Errorf("OAuthFlows[%d]: got %q, want %q", i, flow, tt.expected.OAuthFlows[i])
					}
				}
			}
			if cap.ManualLoginHint != tt.expected.ManualLoginHint {
				t.Errorf("ManualLoginHint: got %q, want %q", cap.ManualLoginHint, tt.expected.ManualLoginHint)
			}
		})
	}
}

// TestToolCapabilityUnknownDefaultsManual verifies unknown tool returns nil.
func TestToolCapabilityUnknownDefaultsManual(t *testing.T) {
	unknownID := ToolID("unknown_tool")
	cap := GetToolCapability(unknownID)
	if cap != nil {
		t.Errorf("GetToolCapability(%q) returned %v, expected nil", unknownID, cap)
	}
}

// TestGetToolCapabilitiesReturnsAllThree verifies the map contains all 3 tools.
func TestGetToolCapabilitiesReturnsAllThree(t *testing.T) {
	caps := GetToolCapabilities()
	if len(caps) != 3 {
		t.Errorf("GetToolCapabilities() returned %d tools, expected 3", len(caps))
	}

	expectedTools := []ToolID{ToolOpencode, ToolClaudeCode, ToolCodex}
	for _, toolID := range expectedTools {
		if _, ok := caps[toolID]; !ok {
			t.Errorf("GetToolCapabilities() missing tool %q", toolID)
		}
	}
}
