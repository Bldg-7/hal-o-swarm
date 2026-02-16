package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// EnvManifest represents the environment manifest schema
type EnvManifest struct {
	Version      string                    `json:"version"`
	Requirements *ManifestRequirements     `json:"requirements"`
	Projects     map[string]*ProjectConfig `json:"projects"`
}

// ManifestRequirements defines all requirement categories
type ManifestRequirements struct {
	Runtime     map[string]string        `json:"runtime"`
	Tools       map[string]string        `json:"tools"`
	EnvVars     map[string]string        `json:"env_vars"`
	AgentConfig *AgentConfigRequirements `json:"agent_config"`
	Context     *ContextRequirements     `json:"context"`
	Git         *GitRequirements         `json:"git"`
	Docs        *DocsRequirements        `json:"docs"`
}

// AgentConfigRequirements defines agent configuration requirements
type AgentConfigRequirements struct {
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
}

// ContextRequirements defines context file/directory requirements
type ContextRequirements struct {
	Files       []string `json:"files"`
	Directories []string `json:"directories"`
}

// GitRequirements defines git configuration requirements
type GitRequirements struct {
	Hooks  []string          `json:"hooks"`
	Config map[string]string `json:"config"`
}

// DocsRequirements defines documentation requirements
type DocsRequirements struct {
	Required    []string `json:"required"`
	Recommended []string `json:"recommended"`
}

// ProjectConfig defines per-project requirement overrides
type ProjectConfig struct {
	Requirements *ManifestRequirements `json:"requirements"`
}

// LoadEnvManifest loads and validates an environment manifest from file
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

// validateEnvManifest performs comprehensive validation of the manifest
func validateEnvManifest(manifest *EnvManifest) error {
	// Validate version
	if manifest.Version == "" {
		return fmt.Errorf("validation error: version is required")
	}
	if !isValidVersionFormat(manifest.Version) {
		return fmt.Errorf("validation error: version must be in format X.Y (e.g., 1.0), got %q", manifest.Version)
	}

	// Validate requirements (must be present)
	if manifest.Requirements == nil {
		return fmt.Errorf("validation error: requirements is required")
	}

	if err := validateRequirements("requirements", manifest.Requirements); err != nil {
		return err
	}

	// Validate projects (optional, but if present, validate each)
	for projectName, projectCfg := range manifest.Projects {
		if projectName == "" {
			return fmt.Errorf("validation error: project name must not be empty")
		}
		if projectCfg == nil {
			return fmt.Errorf("validation error: projects.%s must not be null", projectName)
		}
		if projectCfg.Requirements != nil {
			if err := validateRequirements(fmt.Sprintf("projects.%s.requirements", projectName), projectCfg.Requirements); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateRequirements validates all requirement categories
func validateRequirements(prefix string, reqs *ManifestRequirements) error {
	// Validate runtime versions
	if reqs.Runtime != nil {
		for runtime, version := range reqs.Runtime {
			if runtime == "" {
				return fmt.Errorf("validation error: %s.runtime key must not be empty", prefix)
			}
			if version == "" {
				return fmt.Errorf("validation error: %s.runtime.%s version must not be empty", prefix, runtime)
			}
			if !isValidSemverConstraint(version) {
				return fmt.Errorf("validation error: %s.runtime.%s version must be valid semver constraint (e.g., >=18.0.0), got %q", prefix, runtime, version)
			}
		}
	}

	// Validate tools versions
	if reqs.Tools != nil {
		for tool, version := range reqs.Tools {
			if tool == "" {
				return fmt.Errorf("validation error: %s.tools key must not be empty", prefix)
			}
			if version == "" {
				return fmt.Errorf("validation error: %s.tools.%s version must not be empty", prefix, tool)
			}
			if !isValidSemverConstraint(version) {
				return fmt.Errorf("validation error: %s.tools.%s version must be valid semver constraint (e.g., >=2.30), got %q", prefix, tool, version)
			}
		}
	}

	// Validate environment variables
	if reqs.EnvVars != nil {
		for envVar, status := range reqs.EnvVars {
			if envVar == "" {
				return fmt.Errorf("validation error: %s.env_vars key must not be empty", prefix)
			}
			if status != "required" && status != "optional" {
				return fmt.Errorf("validation error: %s.env_vars.%s must be 'required' or 'optional', got %q", prefix, envVar, status)
			}
		}
	}

	// Validate agent config
	if reqs.AgentConfig != nil {
		if reqs.AgentConfig.Model == "" {
			return fmt.Errorf("validation error: %s.agent_config.model must not be empty", prefix)
		}
		if reqs.AgentConfig.Temperature < 0.0 || reqs.AgentConfig.Temperature > 1.0 {
			return fmt.Errorf("validation error: %s.agent_config.temperature must be between 0.0 and 1.0, got %v", prefix, reqs.AgentConfig.Temperature)
		}
	}

	// Validate context
	if reqs.Context != nil {
		if len(reqs.Context.Files) == 0 && len(reqs.Context.Directories) == 0 {
			return fmt.Errorf("validation error: %s.context must have at least one file or directory", prefix)
		}
		for i, file := range reqs.Context.Files {
			if file == "" {
				return fmt.Errorf("validation error: %s.context.files[%d] must not be empty", prefix, i)
			}
		}
		for i, dir := range reqs.Context.Directories {
			if dir == "" {
				return fmt.Errorf("validation error: %s.context.directories[%d] must not be empty", prefix, i)
			}
		}
	}

	// Validate git
	if reqs.Git != nil {
		for i, hook := range reqs.Git.Hooks {
			if hook == "" {
				return fmt.Errorf("validation error: %s.git.hooks[%d] must not be empty", prefix, i)
			}
			if !isValidGitHook(hook) {
				return fmt.Errorf("validation error: %s.git.hooks[%d] must be a valid git hook name, got %q", prefix, i, hook)
			}
		}
		for key, value := range reqs.Git.Config {
			if key == "" {
				return fmt.Errorf("validation error: %s.git.config key must not be empty", prefix)
			}
			if value == "" {
				return fmt.Errorf("validation error: %s.git.config.%s must not be empty", prefix, key)
			}
		}
	}

	// Validate docs
	if reqs.Docs != nil {
		if len(reqs.Docs.Required) == 0 && len(reqs.Docs.Recommended) == 0 {
			return fmt.Errorf("validation error: %s.docs must have at least one required or recommended document", prefix)
		}
		for i, doc := range reqs.Docs.Required {
			if doc == "" {
				return fmt.Errorf("validation error: %s.docs.required[%d] must not be empty", prefix, i)
			}
		}
		for i, doc := range reqs.Docs.Recommended {
			if doc == "" {
				return fmt.Errorf("validation error: %s.docs.recommended[%d] must not be empty", prefix, i)
			}
		}
	}

	return nil
}

// isValidVersionFormat checks if version is in X.Y format
func isValidVersionFormat(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

// isValidSemverConstraint checks if version is a valid semver constraint
// Accepts formats like: >=18.0.0, ^1.2.3, ~2.3.4, 1.2.3
func isValidSemverConstraint(version string) bool {
	// Remove common constraint operators
	v := strings.TrimPrefix(version, ">=")
	v = strings.TrimPrefix(v, "<=")
	v = strings.TrimPrefix(v, ">")
	v = strings.TrimPrefix(v, "<")
	v = strings.TrimPrefix(v, "^")
	v = strings.TrimPrefix(v, "~")
	v = strings.TrimPrefix(v, "=")

	// Check if remaining string matches semver pattern (X.Y or X.Y.Z)
	semverRegex := regexp.MustCompile(`^\d+(\.\d+)?(\.\d+)?$`)
	return semverRegex.MatchString(v)
}

// isValidGitHook checks if hook name is a valid git hook
func isValidGitHook(hook string) bool {
	validHooks := map[string]bool{
		"pre-commit":         true,
		"prepare-commit-msg": true,
		"commit-msg":         true,
		"post-commit":        true,
		"pre-rebase":         true,
		"post-rewrite":       true,
		"post-checkout":      true,
		"post-merge":         true,
		"pre-push":           true,
		"pre-auto-gc":        true,
	}
	return validHooks[hook]
}
