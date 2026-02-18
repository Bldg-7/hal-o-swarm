package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
)

const policyEngineAgentID = "policy-engine"

type retryState struct {
	count       int
	lastAttempt time.Time
	lastSuccess time.Time
}

type PolicyEngine struct {
	config     config.PolicyConfig
	interval   time.Duration
	tracker    interface{ GetAllSessions() []TrackedSession }
	dispatcher interface {
		DispatchCommand(ctx context.Context, cmd Command) (*CommandResult, error)
	}
	events interface {
		ProcessEvent(agentID string, event Event) error
	}

	ctx    context.Context
	cancel context.CancelFunc

	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	retryMu sync.Mutex
	retries map[string]map[string]retryState

	eventMu  sync.Mutex
	eventSeq uint64
}

func NewPolicyEngine(
	policyConfig config.PolicyConfig,
	tracker interface{ GetAllSessions() []TrackedSession },
	dispatcher interface {
		DispatchCommand(ctx context.Context, cmd Command) (*CommandResult, error)
	},
	events interface {
		ProcessEvent(agentID string, event Event) error
	},
) *PolicyEngine {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := withPolicyDefaults(policyConfig)

	return &PolicyEngine{
		config:     cfg,
		interval:   time.Duration(cfg.CheckIntervalSec) * time.Second,
		tracker:    tracker,
		dispatcher: dispatcher,
		events:     events,
		ctx:        ctx,
		cancel:     cancel,
		retries:    make(map[string]map[string]retryState),
	}
}

func (p *PolicyEngine) Start() {
	p.startOnce.Do(func() {
		interval := p.interval
		if interval <= 0 {
			interval = 30 * time.Second
		}
		ticker := time.NewTicker(interval)

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer ticker.Stop()

			for {
				select {
				case <-p.ctx.Done():
					return
				case <-ticker.C:
					if p.ctx.Err() != nil {
						return
					}
					p.runChecks(time.Now().UTC())
				}
			}
		}()
	})
}

func (p *PolicyEngine) Stop() {
	p.stopOnce.Do(func() {
		p.cancel()

		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(250 * time.Millisecond):
		}
	})
}

func (p *PolicyEngine) runChecks(now time.Time) {
	select {
	case <-p.ctx.Done():
		return
	default:
	}

	if p.tracker == nil || p.dispatcher == nil {
		return
	}

	sessions := p.tracker.GetAllSessions()
	for _, session := range sessions {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		p.evaluateResumeOnIdle(session, now)
		p.evaluateRestartOnCompaction(session, now)
		p.evaluateKillOnCost(session, now)
	}
}

func (p *PolicyEngine) evaluateResumeOnIdle(session TrackedSession, now time.Time) {
	policy := p.config.ResumeOnIdle
	if !policy.Enabled {
		return
	}
	if session.LastActivity.IsZero() || now.Sub(session.LastActivity) < time.Duration(policy.IdleThresholdSec)*time.Second {
		return
	}

	p.tryIntervention(session, "resume_on_idle", policy.MaxRetries, time.Duration(policy.RetryResetSeconds)*time.Second, CommandTypePromptSession)
}

func (p *PolicyEngine) evaluateRestartOnCompaction(session TrackedSession, _ time.Time) {
	policy := p.config.RestartOnCompaction
	if !policy.Enabled {
		return
	}
	if session.TokenUsage.Total < policy.TokenThreshold {
		return
	}

	p.tryIntervention(session, "restart_on_compaction", policy.MaxRetries, time.Duration(policy.RetryResetSeconds)*time.Second, CommandTypeRestartSession)
}

func (p *PolicyEngine) evaluateKillOnCost(session TrackedSession, _ time.Time) {
	policy := p.config.KillOnCost
	if !policy.Enabled {
		return
	}
	if session.SessionCost < policy.CostThresholdUSD {
		return
	}

	p.tryIntervention(session, "kill_on_cost", policy.MaxRetries, time.Duration(policy.RetryResetSeconds)*time.Second, CommandTypeKillSession)
}

func (p *PolicyEngine) tryIntervention(session TrackedSession, policyName string, maxRetries int, retryResetWindow time.Duration, commandType CommandType) {
	now := time.Now().UTC()
	canAttempt, retries := p.canAttempt(session.SessionID, policyName, maxRetries, retryResetWindow, now)
	if !canAttempt {
		return
	}

	result, dispatchErr := p.dispatcher.DispatchCommand(p.ctx, Command{
		Type: commandType,
		Target: CommandTarget{
			NodeID:  session.NodeID,
			Project: session.Project,
		},
		Timeout: 2 * time.Second,
		Args: map[string]interface{}{
			"session_id": session.SessionID,
			"policy":     policyName,
		},
	})

	if dispatchErr != nil {
		retries = p.markFailure(session.SessionID, policyName, now)
		p.emitPolicyAction(session.SessionID, policyName, commandType, "failure", retries, dispatchErr.Error())
		if retries >= maxRetries {
			p.emitRetryCapAlert(session.SessionID, policyName, retries, dispatchErr.Error())
		}
		return
	}

	if result == nil || result.Status != CommandStatusSuccess {
		errorMsg := "command returned non-success status"
		if result != nil && result.Error != "" {
			errorMsg = result.Error
		}
		retries = p.markFailure(session.SessionID, policyName, now)
		p.emitPolicyAction(session.SessionID, policyName, commandType, "failure", retries, errorMsg)
		if retries >= maxRetries {
			p.emitRetryCapAlert(session.SessionID, policyName, retries, errorMsg)
		}
		return
	}

	p.markSuccess(session.SessionID, policyName, now)
	p.emitPolicyAction(session.SessionID, policyName, commandType, "success", retries, "")
}

func (p *PolicyEngine) canAttempt(sessionID, policyName string, maxRetries int, retryResetWindow time.Duration, now time.Time) (bool, int) {
	p.retryMu.Lock()
	defer p.retryMu.Unlock()

	state := p.getRetryStateLocked(sessionID, policyName)
	if maxRetries > 0 && state.count >= maxRetries {
		if retryResetWindow > 0 && now.Sub(state.lastAttempt) >= retryResetWindow {
			state.count = 0
			p.setRetryStateLocked(sessionID, policyName, state)
			return true, 0
		}
		return false, state.count
	}

	return true, state.count
}

func (p *PolicyEngine) markFailure(sessionID, policyName string, now time.Time) int {
	p.retryMu.Lock()
	defer p.retryMu.Unlock()

	state := p.getRetryStateLocked(sessionID, policyName)
	state.count++
	state.lastAttempt = now
	p.setRetryStateLocked(sessionID, policyName, state)

	return state.count
}

func (p *PolicyEngine) markSuccess(sessionID, policyName string, now time.Time) {
	p.retryMu.Lock()
	defer p.retryMu.Unlock()

	state := p.getRetryStateLocked(sessionID, policyName)
	state.count = 0
	state.lastSuccess = now
	p.setRetryStateLocked(sessionID, policyName, state)
}

func (p *PolicyEngine) RetryCount(sessionID, policyName string) int {
	p.retryMu.Lock()
	defer p.retryMu.Unlock()

	state := p.getRetryStateLocked(sessionID, policyName)
	return state.count
}

func (p *PolicyEngine) getRetryStateLocked(sessionID, policyName string) retryState {
	byPolicy, ok := p.retries[sessionID]
	if !ok {
		return retryState{}
	}
	return byPolicy[policyName]
}

func (p *PolicyEngine) setRetryStateLocked(sessionID, policyName string, state retryState) {
	byPolicy, ok := p.retries[sessionID]
	if !ok {
		byPolicy = make(map[string]retryState)
		p.retries[sessionID] = byPolicy
	}
	byPolicy[policyName] = state
}

func (p *PolicyEngine) emitPolicyAction(sessionID, policyName string, action CommandType, result string, retryCount int, lastError string) {
	payload := map[string]interface{}{
		"policy":      policyName,
		"session_id":  sessionID,
		"action":      string(action),
		"result":      result,
		"retry_count": retryCount,
	}
	if lastError != "" {
		payload["last_error"] = lastError
	}
	_ = p.emitPolicyEvent("policy.action", sessionID, payload)
}

func (p *PolicyEngine) emitRetryCapAlert(sessionID, policyName string, retryCount int, lastError string) {
	payload := map[string]interface{}{
		"policy":      policyName,
		"session_id":  sessionID,
		"reason":      "max_retries_reached",
		"retry_count": retryCount,
		"last_error":  lastError,
	}
	_ = p.emitPolicyEvent("policy.alert", sessionID, payload)
	_ = p.emitPolicyEvent("policy.retry_cap", sessionID, payload)
}

func (p *PolicyEngine) emitPolicyEvent(eventType, sessionID string, payload map[string]interface{}) error {
	if p.events == nil {
		return nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	p.eventMu.Lock()
	p.eventSeq++
	seq := p.eventSeq
	p.eventMu.Unlock()

	return p.events.ProcessEvent(policyEngineAgentID, Event{
		ID:        fmt.Sprintf("policy-%d", seq),
		SessionID: sessionID,
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC(),
		Seq:       seq,
	})
}

func withPolicyDefaults(cfg config.PolicyConfig) config.PolicyConfig {
	if cfg.CheckIntervalSec <= 0 {
		cfg.CheckIntervalSec = 30
	}

	if cfg.ResumeOnIdle.IdleThresholdSec <= 0 {
		cfg.ResumeOnIdle.IdleThresholdSec = 300
	}
	if cfg.ResumeOnIdle.MaxRetries <= 0 {
		cfg.ResumeOnIdle.MaxRetries = 3
	}
	if cfg.ResumeOnIdle.RetryResetSeconds <= 0 {
		cfg.ResumeOnIdle.RetryResetSeconds = 3600
	}

	if cfg.RestartOnCompaction.TokenThreshold <= 0 {
		cfg.RestartOnCompaction.TokenThreshold = 180000
	}
	if cfg.RestartOnCompaction.MaxRetries <= 0 {
		cfg.RestartOnCompaction.MaxRetries = 2
	}
	if cfg.RestartOnCompaction.RetryResetSeconds <= 0 {
		cfg.RestartOnCompaction.RetryResetSeconds = 3600
	}

	if cfg.KillOnCost.CostThresholdUSD <= 0 {
		cfg.KillOnCost.CostThresholdUSD = 10
	}
	if cfg.KillOnCost.MaxRetries <= 0 {
		cfg.KillOnCost.MaxRetries = 1
	}
	if cfg.KillOnCost.RetryResetSeconds <= 0 {
		cfg.KillOnCost.RetryResetSeconds = 86400
	}

	return cfg
}
