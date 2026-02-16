package shared

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestAuthStatusJSONRoundTrip tests that all AuthStatus enum values can be serialized and deserialized.
func TestAuthStatusJSONRoundTrip(t *testing.T) {
	statuses := []AuthStatus{
		AuthStatusAuthenticated,
		AuthStatusUnauthenticated,
		AuthStatusNotInstalled,
		AuthStatusError,
		AuthStatusManualRequired,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			// Create a report with the status
			report := AuthStateReport{
				Tool:      ToolIdentifierOpenCode,
				Status:    status,
				Reason:    "test reason",
				CheckedAt: time.Now().UTC(),
			}

			// Marshal to JSON
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			// Unmarshal back
			var unmarshaled AuthStateReport
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Verify status matches
			if unmarshaled.Status != status {
				t.Errorf("status mismatch: got %q, want %q", unmarshaled.Status, status)
			}

			// Verify tool matches
			if unmarshaled.Tool != report.Tool {
				t.Errorf("tool mismatch: got %q, want %q", unmarshaled.Tool, report.Tool)
			}

			// Verify reason matches
			if unmarshaled.Reason != report.Reason {
				t.Errorf("reason mismatch: got %q, want %q", unmarshaled.Reason, report.Reason)
			}
		})
	}
}

// TestToolIdentifierJSONRoundTrip tests that all ToolIdentifier enum values can be serialized and deserialized.
func TestToolIdentifierJSONRoundTrip(t *testing.T) {
	tools := []ToolIdentifier{
		ToolIdentifierOpenCode,
		ToolIdentifierClaudeCode,
		ToolIdentifierCodex,
	}

	for _, tool := range tools {
		t.Run(string(tool), func(t *testing.T) {
			// Create a report with the tool
			report := AuthStateReport{
				Tool:      tool,
				Status:    AuthStatusAuthenticated,
				Reason:    "test",
				CheckedAt: time.Now().UTC(),
			}

			// Marshal to JSON
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			// Unmarshal back
			var unmarshaled AuthStateReport
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Verify tool matches
			if unmarshaled.Tool != tool {
				t.Errorf("tool mismatch: got %q, want %q", unmarshaled.Tool, tool)
			}
		})
	}
}

// TestCredentialPushPayloadJSONRoundTrip tests that CredentialPushPayload can be serialized and deserialized.
func TestCredentialPushPayloadJSONRoundTrip(t *testing.T) {
	payload := CredentialPushPayload{
		TargetNode: "node-1",
		EnvVars: map[string]string{
			"VAR1": "value1",
			"VAR2": "value2",
		},
		Version: 1,
	}

	// Marshal to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	var unmarshaled CredentialPushPayload
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify all fields match
	if unmarshaled.TargetNode != payload.TargetNode {
		t.Errorf("target_node mismatch: got %q, want %q", unmarshaled.TargetNode, payload.TargetNode)
	}

	if unmarshaled.Version != payload.Version {
		t.Errorf("version mismatch: got %d, want %d", unmarshaled.Version, payload.Version)
	}

	if len(unmarshaled.EnvVars) != len(payload.EnvVars) {
		t.Errorf("env_vars length mismatch: got %d, want %d", len(unmarshaled.EnvVars), len(payload.EnvVars))
	}

	for key, value := range payload.EnvVars {
		if unmarshaled.EnvVars[key] != value {
			t.Errorf("env_vars[%q] mismatch: got %q, want %q", key, unmarshaled.EnvVars[key], value)
		}
	}
}

// TestAuthStatusNoSecretField verifies that AuthStateReport has no secret-like fields.
func TestAuthStatusNoSecretField(t *testing.T) {
	forbiddenFields := map[string]bool{
		"key":      true,
		"secret":   true,
		"token":    true,
		"password": true,
	}

	report := AuthStateReport{}
	reportType := reflect.TypeOf(report)

	for i := 0; i < reportType.NumField(); i++ {
		field := reportType.Field(i)
		fieldName := field.Name

		if forbiddenFields[fieldName] {
			t.Errorf("forbidden field found in AuthStateReport: %s", fieldName)
		}
	}
}

// TestCredentialPushPayloadNoSecretField verifies that CredentialPushPayload has no secret-like fields.
func TestCredentialPushPayloadNoSecretField(t *testing.T) {
	forbiddenFields := map[string]bool{
		"key":      true,
		"secret":   true,
		"token":    true,
		"password": true,
	}

	payload := CredentialPushPayload{}
	payloadType := reflect.TypeOf(payload)

	for i := 0; i < payloadType.NumField(); i++ {
		field := payloadType.Field(i)
		fieldName := field.Name

		if forbiddenFields[fieldName] {
			t.Errorf("forbidden field found in CredentialPushPayload: %s", fieldName)
		}
	}
}
