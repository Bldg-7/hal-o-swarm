package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

const defaultAuthReportInterval = 30 * time.Second

type AuthStateSender interface {
	SendAuthState(reports []shared.AuthStateReport) error
}

type AuthReporter struct {
	adapters  []AuthAdapter
	interval  time.Duration
	sender    AuthStateSender
	logger    *zap.Logger
	lastState map[string]shared.AuthStatus
	mu        sync.Mutex
}

type envelopeSender interface {
	SendEnvelope(env *shared.Envelope) error
}

type WSAuthStateSender struct {
	sender envelopeSender
}

type authToolNamer interface {
	ToolID() shared.ToolIdentifier
}

func NewWSAuthStateSender(sender envelopeSender) *WSAuthStateSender {
	return &WSAuthStateSender{sender: sender}
}

func (s *WSAuthStateSender) SendAuthState(reports []shared.AuthStateReport) error {
	if s == nil || s.sender == nil {
		return fmt.Errorf("auth state sender is required")
	}

	payload, err := json.Marshal(reports)
	if err != nil {
		return fmt.Errorf("marshal auth state reports: %w", err)
	}

	env := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeAuthState),
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}

	return s.sender.SendEnvelope(env)
}

func NewAuthReporter(adapters []AuthAdapter, interval time.Duration, sender AuthStateSender, logger *zap.Logger) *AuthReporter {
	if interval <= 0 {
		interval = defaultAuthReportInterval
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	adapterCopy := make([]AuthAdapter, len(adapters))
	copy(adapterCopy, adapters)

	return &AuthReporter{
		adapters:  adapterCopy,
		interval:  interval,
		sender:    sender,
		logger:    logger,
		lastState: make(map[string]shared.AuthStatus),
	}
}

func (r *AuthReporter) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	reports := r.runCheck(ctx)
	_ = r.updateLastState(reports)
	r.sendReports(reports)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reports := r.runCheck(ctx)
			changed := r.updateLastState(reports)
			if changed {
				r.logger.Debug("auth state changed")
			}
			r.sendReports(reports)
		}
	}
}

func (r *AuthReporter) runCheck(ctx context.Context) []shared.AuthStateReport {
	reports := make([]shared.AuthStateReport, 0, len(r.adapters))
	for i, adapter := range r.adapters {
		reports = append(reports, r.checkAdapter(ctx, i, adapter))
	}
	return reports
}

func (r *AuthReporter) checkAdapter(ctx context.Context, idx int, adapter AuthAdapter) (report shared.AuthStateReport) {
	defaultTool := shared.ToolIdentifier(fmt.Sprintf("adapter_%d", idx))
	if named, ok := adapter.(authToolNamer); ok {
		if tool := named.ToolID(); tool != "" {
			defaultTool = tool
		}
	}
	checkedAt := time.Now().UTC()

	if adapter == nil {
		return shared.AuthStateReport{
			Tool:      defaultTool,
			Status:    shared.AuthStatusError,
			Reason:    "auth adapter is nil",
			CheckedAt: checkedAt,
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Warn("auth adapter panic",
				zap.Int("adapter_index", idx),
				zap.Any("panic", recovered),
			)
			report = shared.AuthStateReport{
				Tool:      defaultTool,
				Status:    shared.AuthStatusError,
				Reason:    fmt.Sprintf("auth check panic: %v", recovered),
				CheckedAt: checkedAt,
			}
		}
	}()

	report = adapter.CheckAuth(ctx)
	if report.Tool == "" {
		report.Tool = defaultTool
	}
	if report.Status == "" {
		report.Status = shared.AuthStatusError
		if report.Reason == "" {
			report.Reason = "adapter returned empty status"
		}
	}
	if report.CheckedAt.IsZero() {
		report.CheckedAt = checkedAt
	}

	return report
}

func (r *AuthReporter) updateLastState(reports []shared.AuthStateReport) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastState == nil {
		r.lastState = make(map[string]shared.AuthStatus)
	}

	changed := false
	for _, report := range reports {
		tool := string(report.Tool)
		prev, ok := r.lastState[tool]
		if !ok || prev != report.Status {
			changed = true
		}
		r.lastState[tool] = report.Status
	}

	return changed
}

func (r *AuthReporter) sendReports(reports []shared.AuthStateReport) {
	if r.sender == nil {
		r.logger.Warn("auth state sender not configured")
		return
	}

	if err := r.sender.SendAuthState(reports); err != nil {
		r.logger.Warn("failed to send auth state", zap.Error(err))
	}
}
