package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
)

// TestLoadProjects tests that valid projects are loaded successfully.
func TestLoadProjects(t *testing.T) {
	// Create temporary directories for test projects
	tmpDir := t.TempDir()
	project1Dir := filepath.Join(tmpDir, "project1")
	project2Dir := filepath.Join(tmpDir, "project2")

	if err := os.Mkdir(project1Dir, 0755); err != nil {
		t.Fatalf("failed to create test project directory: %v", err)
	}
	if err := os.Mkdir(project2Dir, 0755); err != nil {
		t.Fatalf("failed to create test project directory: %v", err)
	}

	// Create a valid config
	cfg := &config.AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "test-token",
		OpencodePort:  4096,
		Projects: []struct {
			Name      string `json:"name"`
			Directory string `json:"directory"`
		}{
			{Name: "project1", Directory: project1Dir},
			{Name: "project2", Directory: project2Dir},
		},
	}

	// Create agent
	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	// Verify registry was initialized
	if agent.GetRegistry() == nil {
		t.Fatal("registry is nil")
	}

	// Verify projects were loaded
	if agent.GetRegistry().ProjectCount() != 2 {
		t.Errorf("expected 2 projects, got %d", agent.GetRegistry().ProjectCount())
	}

	// Verify we can retrieve projects
	proj1 := agent.GetRegistry().GetProject("project1")
	if proj1 == nil {
		t.Fatal("project1 not found in registry")
	}
	if proj1.Directory != project1Dir {
		t.Errorf("project1 directory mismatch: expected %s, got %s", project1Dir, proj1.Directory)
	}

	proj2 := agent.GetRegistry().GetProject("project2")
	if proj2 == nil {
		t.Fatal("project2 not found in registry")
	}
	if proj2.Directory != project2Dir {
		t.Errorf("project2 directory mismatch: expected %s, got %s", project2Dir, proj2.Directory)
	}

	// Verify ListProjects works
	projects := agent.GetRegistry().ListProjects()
	if len(projects) != 2 {
		t.Errorf("ListProjects returned %d projects, expected 2", len(projects))
	}
}

// TestLoadProjectsNonexistentDirectory tests that nonexistent project directories fail.
func TestLoadProjectsNonexistentDirectory(t *testing.T) {
	cfg := &config.AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "test-token",
		OpencodePort:  4096,
		Projects: []struct {
			Name      string `json:"name"`
			Directory string `json:"directory"`
		}{
			{Name: "nonexistent", Directory: "/nonexistent/path/that/does/not/exist"},
		},
	}

	_, err := NewAgent(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}

	// Verify error message is clear
	if err.Error() == "" {
		t.Fatal("error message is empty")
	}

	// Check that error mentions the missing directory
	errMsg := err.Error()
	if !contains(errMsg, "does not exist") && !contains(errMsg, "nonexistent") {
		t.Errorf("error message does not mention missing directory: %s", errMsg)
	}
}

// TestLoadProjectsDuplicateNames tests that duplicate project names are rejected.
func TestLoadProjectsDuplicateNames(t *testing.T) {
	tmpDir := t.TempDir()
	project1Dir := filepath.Join(tmpDir, "project1")
	project2Dir := filepath.Join(tmpDir, "project2")

	if err := os.Mkdir(project1Dir, 0755); err != nil {
		t.Fatalf("failed to create test project directory: %v", err)
	}
	if err := os.Mkdir(project2Dir, 0755); err != nil {
		t.Fatalf("failed to create test project directory: %v", err)
	}

	cfg := &config.AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "test-token",
		OpencodePort:  4096,
		Projects: []struct {
			Name      string `json:"name"`
			Directory string `json:"directory"`
		}{
			{Name: "duplicate", Directory: project1Dir},
			{Name: "duplicate", Directory: project2Dir},
		},
	}

	_, err := NewAgent(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate project names, got nil")
	}

	if !contains(err.Error(), "duplicate") {
		t.Errorf("error message does not mention duplicate: %s", err.Error())
	}
}

// TestAgentLifecycle tests agent start/stop lifecycle.
func TestAgentLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")

	if err := os.Mkdir(projectDir, 0755); err != nil {
		t.Fatalf("failed to create test project directory: %v", err)
	}

	cfg := &config.AgentConfig{
		SupervisorURL: "ws://localhost:8420",
		AuthToken:     "test-token",
		OpencodePort:  4096,
		Projects: []struct {
			Name      string `json:"name"`
			Directory string `json:"directory"`
		}{
			{Name: "test", Directory: projectDir},
		},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	// Initially not running
	if agent.IsRunning() {
		t.Fatal("agent should not be running initially")
	}

	// Start agent
	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !agent.IsRunning() {
		t.Fatal("agent should be running after Start")
	}

	// Cannot start twice
	if err := agent.Start(ctx); err == nil {
		t.Fatal("expected error when starting already-running agent")
	}

	// Stop agent
	if err := agent.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if agent.IsRunning() {
		t.Fatal("agent should not be running after Stop")
	}

	// Cannot stop twice
	if err := agent.Stop(ctx); err == nil {
		t.Fatal("expected error when stopping already-stopped agent")
	}
}

// TestProjectRegistryEmptyProjects tests that empty project list is rejected.
func TestProjectRegistryEmptyProjects(t *testing.T) {
	_, err := NewProjectRegistry([]struct {
		Name      string `json:"name"`
		Directory string `json:"directory"`
	}{})

	if err == nil {
		t.Fatal("expected error for empty projects list, got nil")
	}

	if !contains(err.Error(), "no projects") {
		t.Errorf("error message does not mention empty projects: %s", err.Error())
	}
}

// TestProjectRegistryNotADirectory tests that non-directory paths are rejected.
func TestProjectRegistryNotADirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")

	// Create a file instead of directory
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := NewProjectRegistry([]struct {
		Name      string `json:"name"`
		Directory string `json:"directory"`
	}{
		{Name: "test", Directory: filePath},
	})

	if err == nil {
		t.Fatal("expected error for non-directory path, got nil")
	}

	if !contains(err.Error(), "not a directory") {
		t.Errorf("error message does not mention non-directory: %s", err.Error())
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
