package halctl

import (
	"fmt"
)

type AgentMdDiff struct {
	Project  string `json:"project"`
	Local    string `json:"local"`
	Template string `json:"template"`
	Diff     string `json:"diff"`
}

type AgentMdSyncResult struct {
	Project string `json:"project"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func GetAgentMdDiff(client *HTTPClient, project string) (*AgentMdDiff, error) {
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}

	body, err := client.Get("/api/v1/agentmd/diff/" + project)
	if err != nil {
		return nil, err
	}

	var diff AgentMdDiff
	if err := ParseResponse(body, &diff); err != nil {
		return nil, err
	}

	return &diff, nil
}

func SyncAgentMd(client *HTTPClient, project string) (*AgentMdSyncResult, error) {
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}

	payload := map[string]interface{}{
		"type":   "agentmd_sync",
		"target": project,
	}

	body, err := client.Post("/api/v1/commands", payload)
	if err != nil {
		return nil, err
	}

	var result AgentMdSyncResult
	if err := ParseResponse(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
