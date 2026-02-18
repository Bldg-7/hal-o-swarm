package halctl

import (
	"encoding/json"
	"errors"
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

	var cmd commandEnvelope
	if err := ParseResponse(body, &cmd); err != nil {
		return nil, err
	}
	if cmd.CommandID == "" && cmd.Output == "" && cmd.Status != "success" && cmd.Status != "failure" && cmd.Status != "timeout" {
		var direct AgentMdSyncResult
		if err := ParseResponse(body, &direct); err != nil {
			return nil, err
		}
		return &direct, nil
	}
	if cmd.Status != "success" {
		if cmd.Error != "" {
			return nil, errors.New(cmd.Error)
		}
		return nil, fmt.Errorf("agentmd sync failed with status %s", cmd.Status)
	}

	var result AgentMdSyncResult
	if cmd.Output != "" {
		if err := json.Unmarshal([]byte(cmd.Output), &result); err != nil {
			return nil, fmt.Errorf("failed to parse agentmd sync output: %w", err)
		}
	}

	return &result, nil
}
