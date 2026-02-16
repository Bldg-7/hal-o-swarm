package supervisor

import (
	"fmt"
	"strings"
)

type DependencyConfig struct {
	DependsOn []string `json:"depends_on"`
}

type DependencyGraph struct {
	graph        map[string][]string
	reverseGraph map[string][]string
	allProjects  map[string]bool
}

func NewDependencyGraph(config map[string]DependencyConfig) (*DependencyGraph, error) {
	if config == nil {
		config = make(map[string]DependencyConfig)
	}

	dg := &DependencyGraph{
		graph:        make(map[string][]string),
		reverseGraph: make(map[string][]string),
		allProjects:  make(map[string]bool),
	}

	for project, depConfig := range config {
		dg.allProjects[project] = true
		dg.graph[project] = depConfig.DependsOn

		for _, dep := range depConfig.DependsOn {
			dg.allProjects[dep] = true
		}
	}

	for project, deps := range dg.graph {
		for _, dep := range deps {
			dg.reverseGraph[dep] = append(dg.reverseGraph[dep], project)
		}
	}

	if err := dg.Validate(); err != nil {
		return nil, err
	}

	return dg, nil
}

func (dg *DependencyGraph) Validate() error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	path := make([]string, 0)

	for project := range dg.allProjects {
		if !visited[project] {
			if cycle := dg.detectCycleDFS(project, visited, inStack, path); cycle != nil {
				return fmt.Errorf("cycle detected: %s", strings.Join(cycle, " -> "))
			}
		}
	}

	return nil
}

func (dg *DependencyGraph) detectCycleDFS(node string, visited, inStack map[string]bool, path []string) []string {
	visited[node] = true
	inStack[node] = true
	path = append(path, node)

	for _, neighbor := range dg.graph[node] {
		if !visited[neighbor] {
			if cycle := dg.detectCycleDFS(neighbor, visited, inStack, path); cycle != nil {
				return cycle
			}
		} else if inStack[neighbor] {
			cycleStart := -1
			for i, p := range path {
				if p == neighbor {
					cycleStart = i
					break
				}
			}
			if cycleStart != -1 {
				cyclePath := append(path[cycleStart:], neighbor)
				return cyclePath
			}
		}
	}

	inStack[node] = false
	return nil
}

func (dg *DependencyGraph) GetDependencies(project string) []string {
	if deps, ok := dg.graph[project]; ok {
		result := make([]string, len(deps))
		copy(result, deps)
		return result
	}
	return []string{}
}

func (dg *DependencyGraph) GetDependents(project string) []string {
	if dependents, ok := dg.reverseGraph[project]; ok {
		result := make([]string, len(dependents))
		copy(result, dependents)
		return result
	}
	return []string{}
}

func (dg *DependencyGraph) IsReady(project string, tracker *SessionTracker) bool {
	deps := dg.GetDependencies(project)
	if len(deps) == 0 {
		return true
	}

	for _, dep := range deps {
		if !dg.hasCompletedSession(dep, tracker) {
			return false
		}
	}

	return true
}

func (dg *DependencyGraph) hasCompletedSession(project string, tracker *SessionTracker) bool {
	if tracker == nil {
		return false
	}

	sessions := tracker.GetSessionsByProject(project)
	for _, session := range sessions {
		if session.Status != SessionStatusError && session.Status != SessionStatusUnreachable {
			return true
		}
	}

	return false
}

func (dg *DependencyGraph) TriggerDependents(project string, tracker *SessionTracker) ([]string, error) {
	if project == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	dependents := dg.GetDependents(project)
	if len(dependents) == 0 {
		return []string{}, nil
	}

	ready := make([]string, 0)

	for _, dependent := range dependents {
		if dg.IsReady(dependent, tracker) {
			ready = append(ready, dependent)
		}
	}

	return ready, nil
}
