package halctl

import (
	"encoding/json"
	"fmt"
	"time"
)

type SessionJSON struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"node_id"`
	Project   string    `json:"project"`
	Status    string    `json:"status"`
	Tokens    int       `json:"tokens"`
	Cost      float64   `json:"cost"`
	StartedAt time.Time `json:"started_at"`
}

type EventJSON struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

func ListSessions(client *HTTPClient, project, status, nodeID string, limit int) ([]SessionJSON, error) {
	path := "/api/v1/sessions"
	if project != "" || status != "" || nodeID != "" || limit > 0 {
		path += "?"
		if project != "" {
			path += "project=" + project + "&"
		}
		if status != "" {
			path += "status=" + status + "&"
		}
		if nodeID != "" {
			path += "node_id=" + nodeID + "&"
		}
		if limit > 0 {
			path += fmt.Sprintf("limit=%d&", limit)
		}
		if path[len(path)-1] == '&' {
			path = path[:len(path)-1]
		}
	}

	body, err := client.Get(path)
	if err != nil {
		return nil, err
	}

	var sessions []SessionJSON
	if err := ParseResponse(body, &sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

func GetSession(client *HTTPClient, id string) (*SessionJSON, error) {
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}

	body, err := client.Get("/api/v1/sessions/" + id)
	if err != nil {
		return nil, err
	}

	var session SessionJSON
	if err := ParseResponse(body, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func GetSessionLogs(client *HTTPClient, id string, limit int) ([]EventJSON, error) {
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}

	path := fmt.Sprintf("/api/v1/events?session_id=%s", id)
	if limit > 0 {
		path += fmt.Sprintf("&limit=%d", limit)
	}

	body, err := client.Get(path)
	if err != nil {
		return nil, err
	}

	var events []EventJSON
	if err := ParseResponse(body, &events); err != nil {
		return nil, err
	}

	return events, nil
}
