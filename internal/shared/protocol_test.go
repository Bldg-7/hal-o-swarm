package shared

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"action":"test","data":"value"}`)
	env := &Envelope{
		Version:   ProtocolVersion,
		Type:      "event",
		RequestID: "req-123",
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}

	data, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("MarshalEnvelope failed: %v", err)
	}

	unmarshaled, err := UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope failed: %v", err)
	}

	if unmarshaled.Version != env.Version {
		t.Errorf("Version mismatch: got %d, want %d", unmarshaled.Version, env.Version)
	}
	if unmarshaled.Type != env.Type {
		t.Errorf("Type mismatch: got %s, want %s", unmarshaled.Type, env.Type)
	}
	if unmarshaled.RequestID != env.RequestID {
		t.Errorf("RequestID mismatch: got %s, want %s", unmarshaled.RequestID, env.RequestID)
	}
	if unmarshaled.Timestamp != env.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", unmarshaled.Timestamp, env.Timestamp)
	}
	if string(unmarshaled.Payload) != string(env.Payload) {
		t.Errorf("Payload mismatch: got %s, want %s", string(unmarshaled.Payload), string(env.Payload))
	}
}

func TestEnvelopeUnsupportedVersion(t *testing.T) {
	payload := json.RawMessage(`{"test":"data"}`)
	env := &Envelope{
		Version:   999,
		Type:      "event",
		RequestID: "req-456",
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}

	_, err := MarshalEnvelope(env)
	if err == nil {
		t.Fatal("MarshalEnvelope should reject unsupported version")
	}
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestEnvelopeUnsupportedVersionUnmarshal(t *testing.T) {
	data := []byte(`{"version":999,"type":"event","request_id":"req-789","timestamp":1234567890,"payload":{}}`)

	_, err := UnmarshalEnvelope(data)
	if err == nil {
		t.Fatal("UnmarshalEnvelope should reject unsupported version")
	}
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestEnvelopeMissingType(t *testing.T) {
	env := &Envelope{
		Version:   ProtocolVersion,
		Type:      "",
		RequestID: "req-101",
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(`{}`),
	}

	_, err := MarshalEnvelope(env)
	if err == nil {
		t.Fatal("MarshalEnvelope should reject missing type")
	}
	if err != ErrMissingType {
		t.Errorf("Expected ErrMissingType, got %v", err)
	}
}

func TestEnvelopeMissingTimestamp(t *testing.T) {
	env := &Envelope{
		Version:   ProtocolVersion,
		Type:      "event",
		RequestID: "req-102",
		Timestamp: 0,
		Payload:   json.RawMessage(`{}`),
	}

	_, err := MarshalEnvelope(env)
	if err == nil {
		t.Fatal("MarshalEnvelope should reject zero timestamp")
	}
	if err != ErrMissingTimestamp {
		t.Errorf("Expected ErrMissingTimestamp, got %v", err)
	}
}

func TestEnvelopeEmptyPayload(t *testing.T) {
	env := &Envelope{
		Version:   ProtocolVersion,
		Type:      "heartbeat",
		RequestID: "req-103",
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(`{}`),
	}

	data, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("MarshalEnvelope failed: %v", err)
	}

	unmarshaled, err := UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope failed: %v", err)
	}

	if string(unmarshaled.Payload) != "{}" {
		t.Errorf("Payload mismatch: got %s, want {}", string(unmarshaled.Payload))
	}
}

func TestEnvelopeOptionalRequestID(t *testing.T) {
	env := &Envelope{
		Version:   ProtocolVersion,
		Type:      "event",
		RequestID: "",
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(`{"data":"test"}`),
	}

	data, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("MarshalEnvelope failed: %v", err)
	}

	unmarshaled, err := UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope failed: %v", err)
	}

	if unmarshaled.RequestID != "" {
		t.Errorf("RequestID should be empty, got %s", unmarshaled.RequestID)
	}
}

func TestEnvelopeMultipleMessageTypes(t *testing.T) {
	types := []string{"event", "command", "heartbeat"}
	for _, msgType := range types {
		env := &Envelope{
			Version:   ProtocolVersion,
			Type:      msgType,
			RequestID: "req-" + msgType,
			Timestamp: time.Now().Unix(),
			Payload:   json.RawMessage(`{}`),
		}

		data, err := MarshalEnvelope(env)
		if err != nil {
			t.Fatalf("MarshalEnvelope failed for type %s: %v", msgType, err)
		}

		unmarshaled, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("UnmarshalEnvelope failed for type %s: %v", msgType, err)
		}

		if unmarshaled.Type != msgType {
			t.Errorf("Type mismatch for %s: got %s", msgType, unmarshaled.Type)
		}
	}
}
