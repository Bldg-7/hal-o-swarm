package shared

import (
	"encoding/json"
	"fmt"
)

// Envelope represents a protocol message wrapper with version, type, request ID, timestamp, and payload
type Envelope struct {
	Version   int             `json:"version"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// MarshalEnvelope converts an Envelope to JSON bytes
func MarshalEnvelope(env *Envelope) ([]byte, error) {
	if err := validateEnvelope(env); err != nil {
		return nil, err
	}
	return json.Marshal(env)
}

// UnmarshalEnvelope converts JSON bytes to an Envelope with validation
func UnmarshalEnvelope(data []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("failed to unmarshal envelope: %w", err)
	}
	if err := validateEnvelope(&env); err != nil {
		return nil, err
	}
	return &env, nil
}

// validateEnvelope checks that the envelope has all required fields and valid version
func validateEnvelope(env *Envelope) error {
	if env.Version != ProtocolVersion {
		return fmt.Errorf("%w: got %d, expected %d", ErrUnsupportedVersion, env.Version, ProtocolVersion)
	}
	if env.Type == "" {
		return ErrMissingType
	}
	if env.Timestamp == 0 {
		return ErrMissingTimestamp
	}
	return nil
}
