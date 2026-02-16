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

func TestCredentialPushEnvelopeRoundTrip(t *testing.T) {
	payload := CredentialPushPayload{
		TargetNode: "node-1",
		EnvVars: map[string]string{
			"ANTHROPIC_API_KEY": "sk-test-123",
		},
		Version: 1,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	env := &Envelope{
		Version:   ProtocolVersion,
		Type:      string(MessageTypeConfigUpdate),
		RequestID: "req-credential-push-1",
		Timestamp: time.Now().Unix(),
		Payload:   payloadJSON,
	}

	envJSON, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}

	unmarshaledEnv, err := UnmarshalEnvelope(envJSON)
	if err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if unmarshaledEnv.Version != env.Version {
		t.Errorf("envelope version mismatch: got %d, want %d", unmarshaledEnv.Version, env.Version)
	}
	if unmarshaledEnv.Type != env.Type {
		t.Errorf("envelope type mismatch: got %q, want %q", unmarshaledEnv.Type, env.Type)
	}
	if unmarshaledEnv.RequestID != env.RequestID {
		t.Errorf("envelope request_id mismatch: got %q, want %q", unmarshaledEnv.RequestID, env.RequestID)
	}
	if unmarshaledEnv.Timestamp != env.Timestamp {
		t.Errorf("envelope timestamp mismatch: got %d, want %d", unmarshaledEnv.Timestamp, env.Timestamp)
	}

	var unmarshaledPayload CredentialPushPayload
	if err := json.Unmarshal(unmarshaledEnv.Payload, &unmarshaledPayload); err != nil {
		t.Fatalf("failed to unmarshal credential push payload: %v", err)
	}

	if unmarshaledPayload.TargetNode != payload.TargetNode {
		t.Errorf("target_node mismatch: got %q, want %q", unmarshaledPayload.TargetNode, payload.TargetNode)
	}
	if unmarshaledPayload.Version != payload.Version {
		t.Errorf("version mismatch: got %d, want %d", unmarshaledPayload.Version, payload.Version)
	}
	if !reflect.DeepEqual(unmarshaledPayload.EnvVars, payload.EnvVars) {
		t.Errorf("env_vars mismatch: got %#v, want %#v", unmarshaledPayload.EnvVars, payload.EnvVars)
	}
}

func TestCredentialPushRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload CredentialPushPayload
		wantErr bool
	}{
		{
			name: "empty target node",
			payload: CredentialPushPayload{
				TargetNode: "",
				EnvVars: map[string]string{
					"ANTHROPIC_API_KEY": "sk-test-123",
				},
				Version: 1,
			},
			wantErr: true,
		},
		{
			name: "nil env vars",
			payload: CredentialPushPayload{
				TargetNode: "node-1",
				EnvVars:    nil,
				Version:    1,
			},
			wantErr: true,
		},
		{
			name: "empty env vars map",
			payload: CredentialPushPayload{
				TargetNode: "node-1",
				EnvVars:    map[string]string{},
				Version:    1,
			},
			wantErr: true,
		},
		{
			name: "empty env var value",
			payload: CredentialPushPayload{
				TargetNode: "node-1",
				EnvVars: map[string]string{
					"ANTHROPIC_API_KEY": "",
				},
				Version: 1,
			},
			wantErr: true,
		},
		{
			name: "valid payload",
			payload: CredentialPushPayload{
				TargetNode: "node-1",
				EnvVars: map[string]string{
					"ANTHROPIC_API_KEY": "sk-test-123",
				},
				Version: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
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
