package agent

import (
	"context"
	"fmt"
	"sync"
)

type MockOpencodeAdapter struct {
	mu              sync.RWMutex
	nextID          int
	sessions        map[SessionID]SessionInfo
	events          chan Event
	subscribers     map[chan Event]struct{}
	sessionChannels map[SessionID]map[chan Event]struct{}
	forceStreamErr  error
}

func NewMockOpencodeAdapter() *MockOpencodeAdapter {
	return &MockOpencodeAdapter{
		nextID:          1,
		sessions:        make(map[SessionID]SessionInfo),
		events:          make(chan Event, 128),
		subscribers:     make(map[chan Event]struct{}),
		sessionChannels: make(map[SessionID]map[chan Event]struct{}),
	}
}

func (m *MockOpencodeAdapter) SetStreamError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceStreamErr = err
}

func (m *MockOpencodeAdapter) EmitEvent(evt Event) {
	m.events <- evt
}

func (m *MockOpencodeAdapter) SubscribeSessionEvents(ctx context.Context, sessionID SessionID) <-chan Event {
	ch := make(chan Event, 16)
	m.mu.Lock()
	if _, ok := m.sessionChannels[sessionID]; !ok {
		m.sessionChannels[sessionID] = make(map[chan Event]struct{})
	}
	m.sessionChannels[sessionID][ch] = struct{}{}
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		m.mu.Lock()
		if channels, ok := m.sessionChannels[sessionID]; ok {
			delete(channels, ch)
			if len(channels) == 0 {
				delete(m.sessionChannels, sessionID)
			}
		}
		m.mu.Unlock()
		close(ch)
	}()

	return ch
}

func (m *MockOpencodeAdapter) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, sess := range m.sessions {
		out = append(out, sess)
	}
	return out, nil
}

func (m *MockOpencodeAdapter) CreateSession(ctx context.Context, project, prompt string) (SessionID, error) {
	_ = ctx
	m.mu.Lock()
	id := SessionID(fmt.Sprintf("mock-%03d", m.nextID))
	m.nextID++
	status := SessionStatusRunning
	if prompt == "" {
		status = SessionStatusIdle
	}
	info := SessionInfo{ID: id, Project: project, Directory: project, Title: "mock", Status: status}
	m.sessions[id] = info
	m.mu.Unlock()

	m.events <- Event{Type: "session.created", SessionID: id}
	if prompt != "" {
		m.events <- Event{Type: "message.updated", SessionID: id}
	}

	return id, nil
}

func (m *MockOpencodeAdapter) PromptSession(ctx context.Context, sessionID SessionID, message string) error {
	_ = ctx
	_ = message
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	sess.Status = SessionStatusRunning
	m.sessions[sessionID] = sess
	m.events <- Event{Type: "message.updated", SessionID: sessionID}
	return nil
}

func (m *MockOpencodeAdapter) KillSession(ctx context.Context, sessionID SessionID) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	delete(m.sessions, sessionID)
	m.events <- Event{Type: "session.deleted", SessionID: sessionID}
	return nil
}

func (m *MockOpencodeAdapter) SessionStatus(ctx context.Context, sessionID SessionID) (SessionStatus, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[sessionID]
	if !ok {
		return SessionStatusUnknown, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return sess.Status, nil
}

func (m *MockOpencodeAdapter) SubscribeEvents(ctx context.Context) (<-chan Event, error) {
	m.mu.RLock()
	err := m.forceStreamErr
	m.mu.RUnlock()
	if err != nil {
		return nil, mapAdapterError(err)
	}

	consumer := make(chan Event, 32)
	m.mu.Lock()
	m.subscribers[consumer] = struct{}{}
	m.mu.Unlock()

	go func() {
		defer close(consumer)
		for {
			select {
			case <-ctx.Done():
				m.mu.Lock()
				delete(m.subscribers, consumer)
				m.mu.Unlock()
				return
			case evt := <-m.events:
				consumer <- evt
				m.mu.RLock()
				channels := m.sessionChannels[evt.SessionID]
				for ch := range channels {
					select {
					case ch <- evt:
					default:
					}
				}
				m.mu.RUnlock()
			}
		}
	}()

	return consumer, nil
}
