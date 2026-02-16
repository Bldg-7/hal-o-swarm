package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type EnvManifest struct {
	Version      string `json:"version"`
	Requirements []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"requirements"`
}

func LoadEnvManifest(path string) (*EnvManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read env manifest: %w", err)
	}

	var manifest EnvManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse env manifest: %w", err)
	}

	if err := validateEnvManifest(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

func validateEnvManifest(manifest *EnvManifest) error {
	if manifest.Version == "" {
		return fmt.Errorf("validation error: version is required")
	}
	if len(manifest.Requirements) == 0 {
		return fmt.Errorf("validation error: requirements array must not be empty")
	}
	for i, req := range manifest.Requirements {
		if req.Name == "" {
			return fmt.Errorf("validation error: requirements[%d].name is required", i)
		}
		if req.Version == "" {
			return fmt.Errorf("validation error: requirements[%d].version is required", i)
		}
	}
	return nil
}
