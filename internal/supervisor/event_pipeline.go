package supervisor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const persistQueueSize = 1024

type Event struct {
	ID        string
	SessionID string
	Type      string
	Data      json.RawMessage
	Timestamp time.Time
	Seq       uint64
}

type RequestEventRange struct {
	From uint64
	To   uint64
}

type ReplayRequestSender func(agentID string, req RequestEventRange) error

type sequenceStatus int

const (
	sequenceStatusMatch sequenceStatus = iota
	sequenceStatusOld
	sequenceStatusGap
)

type EventPipeline struct {
	logger *zap.Logger
	db     *sql.DB

	sendReplayRequest ReplayRequestSender
	dedup             *eventDedupCache

	mu             sync.Mutex
	agentSequences map[string]uint64
	pendingEvents  map[string]map[uint64]Event

	persistQueue chan Event
	stopCh       chan struct{}
	workers      sync.WaitGroup
}

func NewEventPipeline(db *sql.DB, logger *zap.Logger, sendReplayRequest ReplayRequestSender) (*EventPipeline, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	dedup, err := newEventDedupCache(dedupCacheSizePerAgent)
	if err != nil {
		return nil, fmt.Errorf("create dedup cache: %w", err)
	}

	p := &EventPipeline{
		logger:            logger,
		db:                db,
		sendReplayRequest: sendReplayRequest,
		dedup:             dedup,
		agentSequences:    make(map[string]uint64),
		pendingEvents:     make(map[string]map[uint64]Event),
		persistQueue:      make(chan Event, persistQueueSize),
		stopCh:            make(chan struct{}),
	}

	p.workers.Add(1)
	go p.persistWorker()

	return p, nil
}

func (p *EventPipeline) Close() {
	close(p.stopCh)
	p.workers.Wait()
}

func (p *EventPipeline) ProcessEvent(agentID string, event Event) error {
	if agentID == "" {
		return fmt.Errorf("agent id is required")
	}
	if event.ID == "" {
		return fmt.Errorf("event id is required")
	}

	if p.deduplicateEvent(agentID + ":" + event.ID) {
		return nil
	}

	status, err := p.validateSequence(agentID, event)
	if err != nil {
		return err
	}

	switch status {
	case sequenceStatusOld:
		return nil
	case sequenceStatusGap:
		return p.handleGap(agentID, event)
	case sequenceStatusMatch:
		for _, ordered := range p.consumeInOrder(agentID, event) {
			p.enqueuePersist(ordered)
		}
		return nil
	default:
		return fmt.Errorf("unknown sequence status")
	}
}

func (p *EventPipeline) validateSequence(agentID string, event Event) (sequenceStatus, error) {
	if event.Seq == 0 {
		return sequenceStatusGap, fmt.Errorf("event sequence must be positive")
	}

	p.mu.Lock()
	lastSeq := p.agentSequences[agentID]
	p.mu.Unlock()

	expected := lastSeq + 1
	if event.Seq < expected {
		return sequenceStatusOld, nil
	}
	if event.Seq > expected {
		return sequenceStatusGap, nil
	}

	return sequenceStatusMatch, nil
}

func (p *EventPipeline) deduplicateEvent(eventID string) bool {
	return p.dedup.seen(eventID)
}

func (p *EventPipeline) persistEvent(event Event) error {
	if p.db == nil {
		return nil
	}

	data := string(event.Data)
	if data == "" {
		data = "{}"
	}
	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	_, err := p.db.Exec(
		`INSERT INTO events (id, session_id, type, data, timestamp) VALUES (?, ?, ?, ?, ?)`,
		event.ID,
		event.SessionID,
		event.Type,
		data,
		timestamp.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("persist event %s: %w", event.ID, err)
	}

	return nil
}

func (p *EventPipeline) handleGap(agentID string, event Event) error {
	p.mu.Lock()
	lastSeq := p.agentSequences[agentID]
	expected := lastSeq + 1

	pending := p.pendingEvents[agentID]
	if pending == nil {
		pending = make(map[uint64]Event)
		p.pendingEvents[agentID] = pending
	}
	pending[event.Seq] = event
	p.mu.Unlock()

	p.logger.Warn(
		"event sequence gap detected",
		zap.String("agent_id", agentID),
		zap.Uint64("expected_seq", expected),
		zap.Uint64("received_seq", event.Seq),
	)

	if p.sendReplayRequest != nil {
		if err := p.sendReplayRequest(agentID, RequestEventRange{From: expected, To: event.Seq - 1}); err != nil {
			p.logger.Warn(
				"failed to send replay request",
				zap.String("agent_id", agentID),
				zap.Uint64("from_seq", expected),
				zap.Uint64("to_seq", event.Seq-1),
				zap.Error(err),
			)
		}
	}

	return nil
}

func (p *EventPipeline) consumeInOrder(agentID string, first Event) []Event {
	p.mu.Lock()
	events := []Event{first}
	p.agentSequences[agentID] = first.Seq

	pending := p.pendingEvents[agentID]
	for pending != nil {
		nextSeq := p.agentSequences[agentID] + 1
		nextEvent, ok := pending[nextSeq]
		if !ok {
			break
		}

		delete(pending, nextSeq)
		events = append(events, nextEvent)
		p.agentSequences[agentID] = nextEvent.Seq
	}

	if pending != nil && len(pending) == 0 {
		delete(p.pendingEvents, agentID)
	}
	p.mu.Unlock()

	return events
}

func (p *EventPipeline) enqueuePersist(event Event) {
	select {
	case p.persistQueue <- event:
	default:
		p.logger.Warn(
			"event persist queue full; dropping event",
			zap.String("event_id", event.ID),
			zap.String("session_id", event.SessionID),
		)
	}
}

func (p *EventPipeline) persistWorker() {
	defer p.workers.Done()

	for {
		select {
		case event := <-p.persistQueue:
			if err := p.persistEvent(event); err != nil {
				p.logger.Warn("failed to persist event", zap.String("event_id", event.ID), zap.Error(err))
			}
		case <-p.stopCh:
			for {
				select {
				case event := <-p.persistQueue:
					if err := p.persistEvent(event); err != nil {
						p.logger.Warn("failed to persist event", zap.String("event_id", event.ID), zap.Error(err))
					}
				default:
					return
				}
			}
		}
	}
}

func (p *EventPipeline) LastSequence(agentID string) uint64 {
	p.mu.Lock()
	last := p.agentSequences[agentID]
	p.mu.Unlock()
	return last
}

func (p *EventPipeline) PendingCount(agentID string) int {
	p.mu.Lock()
	pending := p.pendingEvents[agentID]
	p.mu.Unlock()
	return len(pending)
}
