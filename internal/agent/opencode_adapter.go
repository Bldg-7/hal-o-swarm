package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/sst/opencode-sdk-go"
	"github.com/sst/opencode-sdk-go/option"
)

type SessionID string

type SessionStatus string

const (
	SessionStatusUnknown   SessionStatus = "unknown"
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusIdle      SessionStatus = "idle"
	SessionStatusCompacted SessionStatus = "compacted"
	SessionStatusDeleted   SessionStatus = "deleted"
	SessionStatusErrored   SessionStatus = "error"
)

type SessionInfo struct {
	ID        SessionID
	Project   string
	Directory string
	Title     string
	Status    SessionStatus
}

type Event struct {
	Type      string          `json:"type"`
	SessionID SessionID       `json:"session_id"`
	Payload   json.RawMessage `json:"payload"`
}

type OpencodeAdapter interface {
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	CreateSession(ctx context.Context, project, prompt string) (SessionID, error)
	PromptSession(ctx context.Context, sessionID SessionID, message string) error
	KillSession(ctx context.Context, sessionID SessionID) error
	SessionStatus(ctx context.Context, sessionID SessionID) (SessionStatus, error)
	SubscribeEvents(ctx context.Context) (<-chan Event, error)
}

var (
	ErrRecoverable     = errors.New("opencode adapter recoverable error")
	ErrNonRecoverable  = errors.New("opencode adapter non-recoverable error")
	ErrSessionNotFound = errors.New("opencode adapter session not found")
)

type sessionService interface {
	List(ctx context.Context, query opencode.SessionListParams, opts ...option.RequestOption) (*[]opencode.Session, error)
	New(ctx context.Context, params opencode.SessionNewParams, opts ...option.RequestOption) (*opencode.Session, error)
	Prompt(ctx context.Context, id string, params opencode.SessionPromptParams, opts ...option.RequestOption) (*opencode.SessionPromptResponse, error)
	Abort(ctx context.Context, id string, body opencode.SessionAbortParams, opts ...option.RequestOption) (*bool, error)
	Delete(ctx context.Context, id string, body opencode.SessionDeleteParams, opts ...option.RequestOption) (*bool, error)
	Get(ctx context.Context, id string, query opencode.SessionGetParams, opts ...option.RequestOption) (*opencode.Session, error)
}

type eventStream interface {
	Next() bool
	Current() opencode.EventListResponse
	Err() error
	Close() error
}

type eventService interface {
	ListStreaming(ctx context.Context, query opencode.EventListParams, opts ...option.RequestOption) eventStream
}

type opencodeClient interface {
	SessionService() sessionService
	EventService() eventService
}

type sdkClient struct {
	client *opencode.Client
}

func (s *sdkClient) SessionService() sessionService {
	return s.client.Session
}

func (s *sdkClient) EventService() eventService {
	return &sdkEventService{svc: s.client.Event}
}

type sdkEventService struct {
	svc *opencode.EventService
}

func (s *sdkEventService) ListStreaming(ctx context.Context, query opencode.EventListParams, opts ...option.RequestOption) eventStream {
	return s.svc.ListStreaming(ctx, query, opts...)
}

type RealAdapter struct {
	baseURL string
	apiKey  string

	mu               sync.RWMutex
	defaultProject   string
	projectClients   map[string]opencodeClient
	projectDirs      map[string]string
	sessionProjects  map[SessionID]string
	sessionStatuses  map[SessionID]SessionStatus
	subscribers      map[chan Event]struct{}
	sessionConsumers map[SessionID]map[chan Event]struct{}
}

func NewOpencodeAdapter(baseURL, apiKey string) *RealAdapter {
	adapter := &RealAdapter{
		baseURL:          baseURL,
		apiKey:           apiKey,
		projectClients:   make(map[string]opencodeClient),
		projectDirs:      make(map[string]string),
		sessionProjects:  make(map[SessionID]string),
		sessionStatuses:  make(map[SessionID]SessionStatus),
		subscribers:      make(map[chan Event]struct{}),
		sessionConsumers: make(map[SessionID]map[chan Event]struct{}),
	}
	adapter.projectClients[""] = newSDKClient(baseURL, apiKey)
	return adapter
}

func newSDKClient(baseURL, apiKey string) opencodeClient {
	opts := []option.RequestOption{option.WithBaseURL(baseURL)}
	if apiKey != "" {
		opts = append(opts, option.WithHeader("Authorization", "Bearer "+apiKey))
	}
	return &sdkClient{client: opencode.NewClient(opts...)}
}

func (a *RealAdapter) RegisterProjectClient(project, directory, baseURL string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.defaultProject == "" {
		a.defaultProject = project
	}
	a.projectClients[project] = newSDKClient(baseURL, a.apiKey)
	a.projectDirs[project] = directory
}

func (a *RealAdapter) SubscribeSessionEvents(ctx context.Context, sessionID SessionID) <-chan Event {
	ch := make(chan Event, 16)
	a.mu.Lock()
	if _, ok := a.sessionConsumers[sessionID]; !ok {
		a.sessionConsumers[sessionID] = make(map[chan Event]struct{})
	}
	a.sessionConsumers[sessionID][ch] = struct{}{}
	a.mu.Unlock()

	go func() {
		<-ctx.Done()
		a.mu.Lock()
		if consumers, ok := a.sessionConsumers[sessionID]; ok {
			delete(consumers, ch)
			if len(consumers) == 0 {
				delete(a.sessionConsumers, sessionID)
			}
		}
		a.mu.Unlock()
		close(ch)
	}()

	return ch
}

func (a *RealAdapter) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	projects := a.snapshotProjects()
	out := make([]SessionInfo, 0)
	for project, client := range projects {
		directory := a.directoryForProject(project)
		sessions, err := client.SessionService().List(ctx, opencode.SessionListParams{Directory: opencode.F(directory)})
		if err != nil {
			return nil, mapAdapterError(err)
		}
		for _, sess := range *sessions {
			id := SessionID(sess.ID)
			status := parseSessionStatus(sess)
			a.recordSession(id, project, status)
			out = append(out, SessionInfo{
				ID:        id,
				Project:   project,
				Directory: sess.Directory,
				Title:     sess.Title,
				Status:    status,
			})
		}
	}
	return out, nil
}

func (a *RealAdapter) CreateSession(ctx context.Context, project, prompt string) (SessionID, error) {
	client, directory, err := a.clientForProject(project)
	if err != nil {
		return "", err
	}

	created, err := client.SessionService().New(ctx, opencode.SessionNewParams{Directory: opencode.F(directory)})
	if err != nil {
		return "", mapAdapterError(err)
	}

	sessionID := SessionID(created.ID)
	a.recordSession(sessionID, project, SessionStatusRunning)

	if prompt != "" {
		if err := a.PromptSession(ctx, sessionID, prompt); err != nil {
			return "", err
		}
	}

	return sessionID, nil
}

func (a *RealAdapter) PromptSession(ctx context.Context, sessionID SessionID, message string) error {
	project, client, directory, err := a.clientForSession(sessionID)
	if err != nil {
		return err
	}

	_, err = client.SessionService().Prompt(ctx, string(sessionID), opencode.SessionPromptParams{
		Directory: opencode.F(directory),
		Parts: opencode.F([]opencode.SessionPromptParamsPartUnion{
			opencode.TextPartInputParam{
				Type: opencode.F(opencode.TextPartInputTypeText),
				Text: opencode.F(message),
			},
		}),
	})
	if err != nil {
		return mapAdapterError(err)
	}

	a.recordSession(sessionID, project, SessionStatusRunning)
	return nil
}

func (a *RealAdapter) KillSession(ctx context.Context, sessionID SessionID) error {
	project, client, directory, err := a.clientForSession(sessionID)
	if err != nil {
		return err
	}

	if _, err := client.SessionService().Abort(ctx, string(sessionID), opencode.SessionAbortParams{Directory: opencode.F(directory)}); err != nil {
		mapped := mapAdapterError(err)
		if !errors.Is(mapped, ErrSessionNotFound) {
			return mapped
		}
	}

	if _, err := client.SessionService().Delete(ctx, string(sessionID), opencode.SessionDeleteParams{Directory: opencode.F(directory)}); err != nil {
		return mapAdapterError(err)
	}

	a.recordSession(sessionID, project, SessionStatusDeleted)
	return nil
}

func (a *RealAdapter) SessionStatus(ctx context.Context, sessionID SessionID) (SessionStatus, error) {
	project, client, directory, err := a.clientForSession(sessionID)
	if err != nil {
		return SessionStatusUnknown, err
	}

	sess, err := client.SessionService().Get(ctx, string(sessionID), opencode.SessionGetParams{Directory: opencode.F(directory)})
	if err != nil {
		return SessionStatusUnknown, mapAdapterError(err)
	}

	status := parseSessionStatus(*sess)
	a.recordSession(sessionID, project, status)
	return status, nil
}

func (a *RealAdapter) SubscribeEvents(ctx context.Context) (<-chan Event, error) {
	consumer := make(chan Event, 64)
	a.mu.Lock()
	a.subscribers[consumer] = struct{}{}
	a.mu.Unlock()

	go func() {
		<-ctx.Done()
		a.mu.Lock()
		delete(a.subscribers, consumer)
		a.mu.Unlock()
		close(consumer)
	}()

	projects := a.snapshotProjects()
	if len(projects) == 0 {
		return nil, fmt.Errorf("%w: no opencode clients configured", ErrNonRecoverable)
	}

	for project, client := range projects {
		directory := a.directoryForProject(project)
		stream := client.EventService().ListStreaming(ctx, opencode.EventListParams{Directory: opencode.F(directory)})
		if err := mapAdapterError(stream.Err()); err != nil {
			return nil, err
		}

		go a.streamProjectEvents(ctx, project, stream)
	}

	return consumer, nil
}

func (a *RealAdapter) streamProjectEvents(ctx context.Context, project string, stream eventStream) {
	defer stream.Close()

	for stream.Next() {
		raw := stream.Current().JSON.RawJSON()
		event := Event{Type: string(stream.Current().Type), Payload: json.RawMessage(raw)}
		event.SessionID = extractSessionID(raw)
		if event.SessionID != "" {
			status := statusFromEventType(event.Type)
			a.recordSession(event.SessionID, project, status)
		}
		a.dispatchEvent(ctx, event)
	}

	if err := stream.Err(); err != nil {
		a.dispatchEvent(ctx, Event{
			Type:    string(opencode.EventListResponseTypeSessionError),
			Payload: json.RawMessage(`{"error":"stream disconnected"}`),
		})
		_ = mapAdapterError(err)
	}
}

func (a *RealAdapter) dispatchEvent(ctx context.Context, evt Event) {
	a.mu.RLock()
	global := make([]chan Event, 0, len(a.subscribers))
	for ch := range a.subscribers {
		global = append(global, ch)
	}
	var perSession []chan Event
	if evt.SessionID != "" {
		if consumers, ok := a.sessionConsumers[evt.SessionID]; ok {
			perSession = make([]chan Event, 0, len(consumers))
			for ch := range consumers {
				perSession = append(perSession, ch)
			}
		}
	}
	a.mu.RUnlock()

	send := func(ch chan Event) {
		select {
		case ch <- evt:
		case <-ctx.Done():
		default:
		}
	}

	for _, ch := range global {
		send(ch)
	}
	for _, ch := range perSession {
		send(ch)
	}
}

func (a *RealAdapter) snapshotProjects() map[string]opencodeClient {
	a.mu.RLock()
	defer a.mu.RUnlock()
	projects := make(map[string]opencodeClient, len(a.projectClients))
	for name, client := range a.projectClients {
		projects[name] = client
	}
	return projects
}

func (a *RealAdapter) directoryForProject(project string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if directory, ok := a.projectDirs[project]; ok {
		return directory
	}
	return project
}

func (a *RealAdapter) clientForProject(project string) (opencodeClient, string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if client, ok := a.projectClients[project]; ok {
		return client, a.resolveDirectoryLocked(project), nil
	}
	if client, ok := a.projectClients[""]; ok {
		return client, a.resolveDirectoryLocked(project), nil
	}
	return nil, "", fmt.Errorf("%w: unknown project %q", ErrNonRecoverable, project)
}

func (a *RealAdapter) clientForSession(sessionID SessionID) (string, opencodeClient, string, error) {
	a.mu.RLock()
	project, ok := a.sessionProjects[sessionID]
	a.mu.RUnlock()
	if ok {
		client, directory, err := a.clientForProject(project)
		return project, client, directory, err
	}

	projects := a.snapshotProjects()
	for projectName, client := range projects {
		directory := a.directoryForProject(projectName)
		_, err := client.SessionService().Get(context.Background(), string(sessionID), opencode.SessionGetParams{Directory: opencode.F(directory)})
		if err == nil {
			a.recordSession(sessionID, projectName, SessionStatusUnknown)
			return projectName, client, directory, nil
		}
		if !errors.Is(mapAdapterError(err), ErrSessionNotFound) {
			return "", nil, "", mapAdapterError(err)
		}
	}

	return "", nil, "", fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
}

func (a *RealAdapter) resolveDirectoryLocked(project string) string {
	if dir, ok := a.projectDirs[project]; ok {
		return dir
	}
	if project != "" {
		return project
	}
	if a.defaultProject != "" {
		if dir, ok := a.projectDirs[a.defaultProject]; ok {
			return dir
		}
	}
	return ""
}

func (a *RealAdapter) recordSession(sessionID SessionID, project string, status SessionStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionProjects[sessionID] = project
	if status != SessionStatusUnknown {
		a.sessionStatuses[sessionID] = status
	}
}

func parseSessionStatus(sess opencode.Session) SessionStatus {
	raw := sess.JSON.RawJSON()
	if raw == "" {
		return SessionStatusUnknown
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return SessionStatusUnknown
	}
	if stateAny, ok := parsed["state"]; ok {
		if state, ok := stateAny.(string); ok {
			return SessionStatus(state)
		}
	}
	return SessionStatusUnknown
}

func statusFromEventType(eventType string) SessionStatus {
	switch eventType {
	case string(opencode.EventListResponseTypeSessionIdle):
		return SessionStatusIdle
	case string(opencode.EventListResponseTypeSessionCompacted):
		return SessionStatusCompacted
	case string(opencode.EventListResponseTypeSessionDeleted):
		return SessionStatusDeleted
	case string(opencode.EventListResponseTypeSessionError):
		return SessionStatusErrored
	case string(opencode.EventListResponseTypeSessionCreated), string(opencode.EventListResponseTypeSessionUpdated):
		return SessionStatusRunning
	default:
		return SessionStatusUnknown
	}
}

func extractSessionID(raw string) SessionID {
	if raw == "" {
		return ""
	}
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return ""
	}
	props, _ := event["properties"].(map[string]interface{})
	if props == nil {
		return ""
	}
	if sid, ok := props["sessionID"].(string); ok {
		return SessionID(sid)
	}
	if info, ok := props["info"].(map[string]interface{}); ok {
		if sid, ok := info["id"].(string); ok {
			return SessionID(sid)
		}
	}
	return ""
}

func mapAdapterError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrRecoverable, err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %v", ErrRecoverable, err)
	}

	var apiErr *opencode.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w: %v", ErrNonRecoverable, err)
		case http.StatusNotFound:
			return fmt.Errorf("%w: %v", ErrSessionNotFound, err)
		case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusInternalServerError:
			return fmt.Errorf("%w: %v", ErrRecoverable, err)
		default:
			if apiErr.StatusCode >= 500 {
				return fmt.Errorf("%w: %v", ErrRecoverable, err)
			}
			return fmt.Errorf("%w: %v", ErrNonRecoverable, err)
		}
	}

	return fmt.Errorf("%w: %v", ErrRecoverable, err)
}
