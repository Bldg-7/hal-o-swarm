package shared

import "errors"

// Protocol version constant
const ProtocolVersion = 1

// Error types for protocol validation
var (
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
	ErrMissingType        = errors.New("missing required field: type")
	ErrMissingTimestamp   = errors.New("missing required field: timestamp")
	ErrInvalidPayload     = errors.New("invalid payload")
)

// MessageType represents the type of message being sent
type MessageType string

const (
	MessageTypeEvent        MessageType = "event"
	MessageTypeCommand      MessageType = "command"
	MessageTypeHeartbeat    MessageType = "heartbeat"
	MessageTypeRegister     MessageType = "register"
	MessageTypeAuthState    MessageType = "auth_state"
	MessageTypeConfigUpdate MessageType = "config_update"
)
