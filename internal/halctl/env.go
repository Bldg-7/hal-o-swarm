package halctl

import (
	"encoding/json"
	"errors"
	"fmt"
)

type EnvCheckResult struct {
	Project string                 `json:"project"`
	Status  string                 `json:"status"`
	Issues  []string               `json:"issues,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type EnvProvisionResult struct {
	Project string                 `json:"project"`
	Status  string                 `json:"status"`
	Changes []string               `json:"changes,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type commandEnvelope struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
}

func GetEnvStatus(client *HTTPClient, project string) (*EnvCheckResult, error) {
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}

	body, err := client.Get("/api/v1/env/status/" + project)
	if err != nil {
		return nil, err
	}

	var result EnvCheckResult
	if err := ParseResponse(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func CheckEnv(client *HTTPClient, project string) (*EnvCheckResult, error) {
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}

	payload := map[string]interface{}{
		"type":   "env_check",
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
		var direct EnvCheckResult
		if err := ParseResponse(body, &direct); err != nil {
			return nil, err
		}
		return &direct, nil
	}
	if cmd.Status != "success" {
		if cmd.Error != "" {
			return nil, errors.New(cmd.Error)
		}
		return nil, fmt.Errorf("env check failed with status %s", cmd.Status)
	}

	var result EnvCheckResult
	if cmd.Output != "" {
		if err := json.Unmarshal([]byte(cmd.Output), &result); err != nil {
			return nil, fmt.Errorf("failed to parse env check output: %w", err)
		}
	}

	return &result, nil
}

func ProvisionEnv(client *HTTPClient, project string) (*EnvProvisionResult, error) {
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}

	payload := map[string]interface{}{
		"type":   "env_provision",
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
		var direct EnvProvisionResult
		if err := ParseResponse(body, &direct); err != nil {
			return nil, err
		}
		return &direct, nil
	}
	if cmd.Status != "success" {
		if cmd.Error != "" {
			return nil, errors.New(cmd.Error)
		}
		return nil, fmt.Errorf("env provision failed with status %s", cmd.Status)
	}

	var result EnvProvisionResult
	if cmd.Output != "" {
		if err := json.Unmarshal([]byte(cmd.Output), &result); err != nil {
			return nil, fmt.Errorf("failed to parse env provision output: %w", err)
		}
	}

	return &result, nil
}
