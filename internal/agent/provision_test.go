package agent

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"github.com/Bldg-7/hal-o-swarm/internal/shared"
)

func newTestManifest() *config.EnvManifest {
	return &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			AgentConfig: &config.AgentConfigRequirements{
				Model:       "claude-sonnet-4-20250514",
				Temperature: 0.7,
			},
			Context: &config.ContextRequirements{
				Files:       []string{"context/rules.md"},
				Directories: []string{"context/"},
			},
			Docs: &config.DocsRequirements{
				Required: []string{"README.md", "CONTRIBUTING.md"},
			},
			Git: &config.GitRequirements{
				Hooks: []string{"pre-commit"},
			},
		},
	}
}

func TestProvisionSafeCreatesAgentMD(t *testing.T) {
	projectDir := t.TempDir()
	manifest := newTestManifest()

	prov := NewProvisioner(projectDir, "test-project", manifest, "", nil)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	agentMD := filepath.Join(projectDir, "AGENT.md")
	if _, err := os.Stat(agentMD); os.IsNotExist(err) {
		t.Fatal("AGENT.md was not created")
	}

	data, _ := os.ReadFile(agentMD)
	if !findSubstring(string(data), "test-project") {
		t.Error("AGENT.md does not contain project name")
	}

	found := false
	for _, a := range result.Applied {
		if a.Item == "AGENT.md" && a.Action == "created" {
			found = true
		}
	}
	if !found {
		t.Error("AGENT.md not in applied actions")
	}
}

func TestProvisionSafeCreatesContextFiles(t *testing.T) {
	projectDir := t.TempDir()
	manifest := newTestManifest()

	prov := NewProvisioner(projectDir, "test-project", manifest, "", nil)
	_, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	contextDir := filepath.Join(projectDir, "context")
	if info, err := os.Stat(contextDir); err != nil || !info.IsDir() {
		t.Error("context/ directory was not created")
	}

	rulesFile := filepath.Join(projectDir, "context", "rules.md")
	if _, err := os.Stat(rulesFile); os.IsNotExist(err) {
		t.Error("context/rules.md was not created")
	}
}

func TestProvisionSafeCreatesDocs(t *testing.T) {
	projectDir := t.TempDir()
	manifest := newTestManifest()

	prov := NewProvisioner(projectDir, "test-project", manifest, "", nil)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	for _, doc := range []string{"README.md", "CONTRIBUTING.md"} {
		path := filepath.Join(projectDir, doc)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("%s was not created", doc)
		}
	}

	docActions := 0
	for _, a := range result.Applied {
		if a.Category == shared.DriftCategoryDocs {
			docActions++
		}
	}
	if docActions != 2 {
		t.Errorf("expected 2 doc actions, got %d", docActions)
	}
}

func TestProvisionSafeInstallsGitHook(t *testing.T) {
	projectDir := t.TempDir()
	manifest := newTestManifest()

	if err := os.MkdirAll(filepath.Join(projectDir, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}

	prov := NewProvisioner(projectDir, "test-project", manifest, "", nil)
	_, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	hookPath := filepath.Join(projectDir, ".git", "hooks", "pre-commit")
	info, err := os.Stat(hookPath)
	if os.IsNotExist(err) {
		t.Fatal("pre-commit hook was not created")
	}

	if info.Mode().Perm()&0111 == 0 {
		t.Error("pre-commit hook is not executable")
	}
}

func TestProvisionRiskyEmitsEvent(t *testing.T) {
	projectDir := t.TempDir()
	manifest := &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			Runtime: map[string]string{
				"java": ">=17",
			},
			Tools: map[string]string{
				"docker": ">=20.0",
			},
		},
	}

	var mu sync.Mutex
	var events []shared.ProvisionEvent
	emitter := func(evt shared.ProvisionEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	}

	prov := NewProvisioner(projectDir, "test-project", manifest, "", emitter)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if len(result.Pending) != 2 {
		t.Fatalf("expected 2 pending items, got %d", len(result.Pending))
	}

	mu.Lock()
	eventCount := len(events)
	mu.Unlock()
	if eventCount != 2 {
		t.Fatalf("expected 2 events, got %d", eventCount)
	}

	for _, evt := range events {
		if evt.Type != "provision.manual_required" {
			t.Errorf("expected event type provision.manual_required, got %s", evt.Type)
		}
		if evt.Data.ApprovalToken == "" {
			t.Error("event missing approval token")
		}
	}

	for _, p := range result.Pending {
		if p.Reason != "manual_approval_required" {
			t.Errorf("expected reason manual_approval_required, got %s", p.Reason)
		}
	}

	if result.Status != shared.ProvisionStatusPartial {
		t.Errorf("expected status partial, got %s", result.Status)
	}

	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied actions for risky-only manifest, got %d", len(result.Applied))
	}
}

func TestProvisionRiskyJavaNoInstall(t *testing.T) {
	projectDir := t.TempDir()
	manifest := &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			Runtime: map[string]string{
				"java": ">=17",
			},
		},
	}

	var events []shared.ProvisionEvent
	emitter := func(evt shared.ProvisionEvent) {
		events = append(events, evt)
	}

	prov := NewProvisioner(projectDir, "test-project", manifest, "", emitter)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if len(result.Applied) != 0 {
		t.Error("risky items should NOT be auto-applied")
	}

	if len(result.Pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(result.Pending))
	}

	if result.Pending[0].Item != "java" {
		t.Errorf("expected pending item java, got %s", result.Pending[0].Item)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "provision.manual_required" {
		t.Errorf("expected manual_required event, got %s", events[0].Type)
	}
	if events[0].Data.Item != "java" {
		t.Errorf("expected event for java, got %s", events[0].Data.Item)
	}
}

func TestProvisionIdempotent(t *testing.T) {
	projectDir := t.TempDir()
	manifest := newTestManifest()

	prov := NewProvisioner(projectDir, "test-project", manifest, "", nil)

	result1, err := prov.Provision()
	if err != nil {
		t.Fatalf("first Provision failed: %v", err)
	}
	firstApplied := len(result1.Applied)
	if firstApplied == 0 {
		t.Fatal("first run should have applied fixes")
	}

	result2, err := prov.Provision()
	if err != nil {
		t.Fatalf("second Provision failed: %v", err)
	}

	if len(result2.Applied) != 0 {
		t.Errorf("second run should apply 0 fixes (idempotent), got %d", len(result2.Applied))
	}

	if result2.Status != shared.ProvisionStatusCompleted {
		t.Errorf("expected completed status on second run, got %s", result2.Status)
	}
}

func TestProvisionWithTemplate(t *testing.T) {
	projectDir := t.TempDir()
	templateDir := t.TempDir()

	customContent := "# Custom AGENT.md for {{PROJECT_NAME}}\nCustom template content.\n"
	if err := os.WriteFile(filepath.Join(templateDir, "AGENT.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			AgentConfig: &config.AgentConfigRequirements{
				Model:       "claude-sonnet-4-20250514",
				Temperature: 0.7,
			},
		},
	}

	prov := NewProvisioner(projectDir, "my-project", manifest, templateDir, nil)
	_, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectDir, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !findSubstring(content, "Custom template content") {
		t.Error("AGENT.md should use custom template content")
	}
	if !findSubstring(content, "my-project") {
		t.Error("AGENT.md should have project name substituted")
	}
}

func TestProvisionNilManifest(t *testing.T) {
	projectDir := t.TempDir()

	prov := NewProvisioner(projectDir, "test-project", nil, "", nil)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if result.Status != shared.ProvisionStatusCompleted {
		t.Errorf("expected completed for nil manifest, got %s", result.Status)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied for nil manifest, got %d", len(result.Applied))
	}
}

func TestProvisionMixedSafeAndRisky(t *testing.T) {
	projectDir := t.TempDir()
	manifest := &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			AgentConfig: &config.AgentConfigRequirements{
				Model:       "claude-sonnet-4-20250514",
				Temperature: 0.7,
			},
			Docs: &config.DocsRequirements{
				Required: []string{"README.md"},
			},
			Runtime: map[string]string{
				"node":   ">=18.0.0",
				"python": ">=3.10",
			},
		},
	}

	var events []shared.ProvisionEvent
	emitter := func(evt shared.ProvisionEvent) {
		events = append(events, evt)
	}

	prov := NewProvisioner(projectDir, "test-project", manifest, "", emitter)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if result.Status != shared.ProvisionStatusPartial {
		t.Errorf("expected partial status for mixed, got %s", result.Status)
	}

	if len(result.Applied) < 2 {
		t.Errorf("expected at least 2 applied (AGENT.md + README.md), got %d", len(result.Applied))
	}

	if len(result.Pending) != 2 {
		t.Errorf("expected 2 pending (node + python), got %d", len(result.Pending))
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestProvisionDoesNotOverwriteExisting(t *testing.T) {
	projectDir := t.TempDir()

	originalContent := "# My custom README\nDo not overwrite me.\n"
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			Docs: &config.DocsRequirements{
				Required: []string{"README.md"},
			},
		},
	}

	prov := NewProvisioner(projectDir, "test-project", manifest, "", nil)
	result, err := prov.Provision()
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(projectDir, "README.md"))
	if string(data) != originalContent {
		t.Error("existing README.md was overwritten")
	}

	for _, a := range result.Applied {
		if a.Item == "README.md" {
			t.Error("README.md should not appear in applied (already exists)")
		}
	}
}

func TestProvisionResultTimestamp(t *testing.T) {
	projectDir := t.TempDir()
	manifest := &config.EnvManifest{
		Version:      "1.0",
		Requirements: &config.ManifestRequirements{},
	}

	prov := NewProvisioner(projectDir, "test", manifest, "", nil)
	result, err := prov.Provision()
	if err != nil {
		t.Fatal(err)
	}

	if result.Timestamp.IsZero() {
		t.Error("result timestamp should not be zero")
	}
}

func TestProvisionGitHookFromTemplate(t *testing.T) {
	projectDir := t.TempDir()
	templateDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(templateDir, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	hookContent := "#!/bin/sh\necho 'custom pre-commit'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(templateDir, "hooks", "pre-commit"), []byte(hookContent), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(projectDir, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}

	manifest := &config.EnvManifest{
		Version: "1.0",
		Requirements: &config.ManifestRequirements{
			Git: &config.GitRequirements{
				Hooks: []string{"pre-commit"},
			},
		},
	}

	prov := NewProvisioner(projectDir, "test", manifest, templateDir, nil)
	_, err := prov.Provision()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(projectDir, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != hookContent {
		t.Error("hook should use template content")
	}
}
