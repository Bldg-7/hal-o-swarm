package halctl

import (
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

	var result EnvCheckResult
	if err := ParseResponse(body, &result); err != nil {
		return nil, err
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

	var result EnvProvisionResult
	if err := ParseResponse(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
