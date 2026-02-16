package halctl

import (
	"fmt"
	"time"
)

type NodeJSON struct {
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	ConnectedAt   time.Time `json:"connected_at"`
}

func ListNodes(client *HTTPClient) ([]NodeJSON, error) {
	body, err := client.Get("/api/v1/nodes")
	if err != nil {
		return nil, err
	}

	var nodes []NodeJSON
	if err := ParseResponse(body, &nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

func GetNode(client *HTTPClient, id string) (*NodeJSON, error) {
	if id == "" {
		return nil, fmt.Errorf("node id is required")
	}

	body, err := client.Get("/api/v1/nodes/" + id)
	if err != nil {
		return nil, err
	}

	var node NodeJSON
	if err := ParseResponse(body, &node); err != nil {
		return nil, err
	}

	return &node, nil
}
