package shared

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const correlationIDKey contextKey = "correlation_id"

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// GetCorrelationID retrieves the correlation ID from context, or generates a new one if not present
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok && id != "" {
		return id
	}
	return uuid.New().String()
}

// LogWithContext logs a message with correlation ID from context
func LogWithContext(ctx context.Context, logger *zap.Logger, msg string, fields ...zap.Field) {
	if logger == nil {
		return
	}
	id := GetCorrelationID(ctx)
	fields = append(fields, zap.String("correlation_id", id))
	logger.Info(msg, fields...)
}

// LogErrorWithContext logs an error with correlation ID from context
func LogErrorWithContext(ctx context.Context, logger *zap.Logger, msg string, err error, fields ...zap.Field) {
	if logger == nil {
		return
	}
	id := GetCorrelationID(ctx)
	fields = append(fields, zap.String("correlation_id", id), zap.Error(err))
	logger.Error(msg, fields...)
}
