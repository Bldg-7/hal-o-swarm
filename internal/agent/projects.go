package agent

import (
	"fmt"
	"os"
	"sync"
)

// ProjectInfo represents a single project configuration.
type ProjectInfo struct {
	Name      string
	Directory string
}

// ProjectRegistry manages the set of projects available to this agent.
// It validates that all project directories exist on the filesystem.
type ProjectRegistry struct {
	projects map[string]*ProjectInfo
	mu       sync.RWMutex
}

// NewProjectRegistry creates a new ProjectRegistry from a list of project configs.
// It validates that each project directory exists.
func NewProjectRegistry(projects []struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
}) (*ProjectRegistry, error) {
	if len(projects) == 0 {
		return nil, fmt.Errorf("project registry: no projects provided")
	}

	registry := &ProjectRegistry{
		projects: make(map[string]*ProjectInfo),
	}

	for i, proj := range projects {
		// Validate project name
		if proj.Name == "" {
			return nil, fmt.Errorf("project registry: projects[%d].name is empty", i)
		}

		// Validate project directory
		if proj.Directory == "" {
			return nil, fmt.Errorf("project registry: projects[%d].directory is empty", i)
		}

		// Check that directory exists
		info, err := os.Stat(proj.Directory)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("project registry: projects[%d] (%s) directory does not exist: %s", i, proj.Name, proj.Directory)
			}
			return nil, fmt.Errorf("project registry: failed to stat projects[%d] (%s) directory: %w", i, proj.Name, err)
		}

		// Ensure it's a directory
		if !info.IsDir() {
			return nil, fmt.Errorf("project registry: projects[%d] (%s) path is not a directory: %s", i, proj.Name, proj.Directory)
		}

		// Check for duplicates
		if _, exists := registry.projects[proj.Name]; exists {
			return nil, fmt.Errorf("project registry: duplicate project name: %s", proj.Name)
		}

		registry.projects[proj.Name] = &ProjectInfo{
			Name:      proj.Name,
			Directory: proj.Directory,
		}
	}

	return registry, nil
}

// GetProject returns a project by name, or nil if not found.
func (r *ProjectRegistry) GetProject(name string) *ProjectInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.projects[name]
}

// ListProjects returns all registered projects.
func (r *ProjectRegistry) ListProjects() []*ProjectInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	projects := make([]*ProjectInfo, 0, len(r.projects))
	for _, proj := range r.projects {
		projects = append(projects, proj)
	}
	return projects
}

// ProjectCount returns the number of registered projects.
func (r *ProjectRegistry) ProjectCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.projects)
}
