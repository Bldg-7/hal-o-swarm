package supervisor

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
)

type policyTestTracker struct {
	mu       sync.Mutex
	sessions []TrackedSession
}

func (t *policyTestTracker) GetAllSessions() []TrackedSession {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]TrackedSession, len(t.sessions))
	copy(out, t.sessions)
	return out
}

type policyTestDispatcher struct {
	mu      sync.Mutex
	calls   []Command
	results []*CommandResult
	err     error
}

func (d *policyTestDispatcher) DispatchCommand(_ context.Context, cmd Command) (*CommandResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.calls = append(d.calls, cmd)
	if d.err != nil {
		return nil, d.err
	}
	if len(d.results) == 0 {
		return &CommandResult{Status: CommandStatusSuccess, Timestamp: time.Now().UTC()}, nil
	}
	result := d.results[0]
	d.results = d.results[1:]
	if result == nil {
		return nil, nil
	}
	copyResult := *result
	if copyResult.Timestamp.IsZero() {
		copyResult.Timestamp = time.Now().UTC()
	}
	return &copyResult, nil
}

func (d *policyTestDispatcher) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *policyTestDispatcher) callTypes() []CommandType {
	d.mu.Lock()
	defer d.mu.Unlock()

	types := make([]CommandType, 0, len(d.calls))
	for _, call := range d.calls {
		types = append(types, call.Type)
	}
	return types
}

type policyTestEvents struct {
	mu     sync.Mutex
	events []Event
}

func (e *policyTestEvents) ProcessEvent(_ string, event Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	return nil
}

func (e *policyTestEvents) hasEventType(eventType string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, event := range e.events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func (e *policyTestEvents) hasAlertReason(reason string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, event := range e.events {
		if event.Type != "policy.alert" {
			continue
		}
		payload := map[string]interface{}{}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			continue
		}
		if payload["reason"] == reason {
			return true
		}
	}

	return false
}

func TestPolicyEngineResumeOnIdle(t *testing.T) {
	now := time.Now().UTC()
	tracker := &policyTestTracker{sessions: []TrackedSession{{
		SessionID:    "session-idle",
		NodeID:       "node-1",
		Project:      "proj-a",
		Status:       SessionStatusIdle,
		LastActivity: now.Add(-2 * time.Second),
	}}}
	dispatcher := &policyTestDispatcher{results: []*CommandResult{{Status: CommandStatusSuccess}}}
	events := &policyTestEvents{}

	engine := NewPolicyEngine(config.PolicyConfig{
		CheckIntervalSec: 1,
		ResumeOnIdle: config.IdlePolicyConfig{
			Enabled:           true,
			IdleThresholdSec:  1,
			MaxRetries:        3,
			RetryResetSeconds: 10,
		},
	}, tracker, dispatcher, events)

	engine.interval = 10 * time.Millisecond
	engine.Start()
	defer engine.Stop()

	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dispatcher.callCount() > 0 && events.hasEventType("policy.action") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dispatcher.callCount() == 0 {
		t.Fatal("expected resume intervention command")
	}
	if got := dispatcher.callTypes()[0]; got != CommandTypePromptSession {
		t.Fatalf("expected first command type %q, got %q", CommandTypePromptSession, got)
	}
	if count := engine.RetryCount("session-idle", "resume_on_idle"); count != 0 {
		t.Fatalf("expected retry count reset to 0 on success, got %d", count)
	}
	if !events.hasEventType("policy.action") {
		t.Fatal("expected policy.action event")
	}
}

func TestPolicyEngineRetryCap(t *testing.T) {
	now := time.Now().UTC()
	tracker := &policyTestTracker{sessions: []TrackedSession{{
		SessionID:    "session-fail",
		NodeID:       "node-2",
		Project:      "proj-b",
		Status:       SessionStatusIdle,
		LastActivity: now.Add(-2 * time.Second),
	}}}
	dispatcher := &policyTestDispatcher{results: []*CommandResult{
		{Status: CommandStatusFailure, Error: "command timeout"},
		{Status: CommandStatusFailure, Error: "command timeout"},
		{Status: CommandStatusFailure, Error: "command timeout"},
		{Status: CommandStatusFailure, Error: "command timeout"},
	}}
	events := &policyTestEvents{}

	engine := NewPolicyEngine(config.PolicyConfig{
		CheckIntervalSec: 1,
		ResumeOnIdle: config.IdlePolicyConfig{
			Enabled:           true,
			IdleThresholdSec:  1,
			MaxRetries:        2,
			RetryResetSeconds: 100,
		},
	}, tracker, dispatcher, events)

	engine.interval = 10 * time.Millisecond
	engine.Start()
	defer engine.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dispatcher.callCount() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dispatcher.callCount() != 2 {
		t.Fatalf("expected exactly 2 dispatch attempts before retry cap, got %d", dispatcher.callCount())
	}

	time.Sleep(120 * time.Millisecond)
	if dispatcher.callCount() != 2 {
		t.Fatalf("expected no dispatches after retry cap reached, got %d", dispatcher.callCount())
	}

	if count := engine.RetryCount("session-fail", "resume_on_idle"); count != 2 {
		t.Fatalf("expected retry count at cap 2, got %d", count)
	}
	if !events.hasAlertReason("max_retries_reached") {
		t.Fatal("expected policy.alert with max_retries_reached reason")
	}
	if !events.hasEventType("policy.retry_cap") {
		t.Fatal("expected policy.retry_cap event")
	}
}

func TestPolicyEngineRestartAndKillTriggers(t *testing.T) {
	now := time.Now().UTC()
	tracker := &policyTestTracker{sessions: []TrackedSession{
		{
			SessionID:    "session-compact",
			NodeID:       "node-3",
			Project:      "proj-c",
			Status:       SessionStatusRunning,
			LastActivity: now,
			TokenUsage:   TokenUsage{Total: 200000},
		},
		{
			SessionID:    "session-cost",
			NodeID:       "node-4",
			Project:      "proj-d",
			Status:       SessionStatusRunning,
			LastActivity: now,
			SessionCost:  12.5,
		},
	}}
	dispatcher := &policyTestDispatcher{results: []*CommandResult{{Status: CommandStatusSuccess}, {Status: CommandStatusSuccess}}}
	engine := NewPolicyEngine(config.PolicyConfig{
		CheckIntervalSec: 1,
		RestartOnCompaction: config.CompactionPolicyConfig{
			Enabled:           true,
			TokenThreshold:    180000,
			MaxRetries:        2,
			RetryResetSeconds: 60,
		},
		KillOnCost: config.CostPolicyConfig{
			Enabled:           true,
			CostThresholdUSD:  10.0,
			MaxRetries:        1,
			RetryResetSeconds: 60,
		},
	}, tracker, dispatcher, &policyTestEvents{})

	engine.interval = 10 * time.Millisecond
	engine.Start()
	defer engine.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dispatcher.callCount() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dispatcher.callCount() < 2 {
		t.Fatalf("expected restart and kill commands, got %d calls", dispatcher.callCount())
	}

	types := dispatcher.callTypes()
	seenRestart := false
	seenKill := false
	for _, typ := range types {
		if typ == CommandTypeRestartSession {
			seenRestart = true
		}
		if typ == CommandTypeKillSession {
			seenKill = true
		}
	}
	if !seenRestart {
		t.Fatal("expected restart_session command")
	}
	if !seenKill {
		t.Fatal("expected kill_session command")
	}
}

func TestPolicyEngineStopStopsChecks(t *testing.T) {
	now := time.Now().UTC()
	tracker := &policyTestTracker{sessions: []TrackedSession{{
		SessionID:    "session-stop",
		NodeID:       "node-5",
		Project:      "proj-e",
		Status:       SessionStatusIdle,
		LastActivity: now.Add(-2 * time.Second),
	}}}
	dispatcher := &policyTestDispatcher{results: []*CommandResult{{Status: CommandStatusFailure, Error: "fail"}}}

	engine := NewPolicyEngine(config.PolicyConfig{
		CheckIntervalSec: 1,
		ResumeOnIdle: config.IdlePolicyConfig{
			Enabled:           true,
			IdleThresholdSec:  1,
			MaxRetries:        10,
			RetryResetSeconds: 100,
		},
	}, tracker, dispatcher, &policyTestEvents{})

	engine.interval = 10 * time.Millisecond
	engine.Start()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if dispatcher.callCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	beforeStop := dispatcher.callCount()
	engine.Stop()
	atStop := dispatcher.callCount()
	time.Sleep(120 * time.Millisecond)
	afterStop := dispatcher.callCount()

	if beforeStop == 0 {
		t.Fatal("expected at least one check before stop")
	}
	if atStop < beforeStop {
		t.Fatalf("expected call count not to decrease, before=%d at_stop=%d", beforeStop, atStop)
	}
	if afterStop != atStop {
		t.Fatalf("expected no additional checks after stop, at_stop=%d after=%d", atStop, afterStop)
	}
}
