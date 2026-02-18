package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"github.com/Bldg-7/hal-o-swarm/internal/shared"
)

const (
	envMarkerComment = "# hal-o-swarm managed"
	defaultAgentMD   = `# AGENT.md

## Project: {{PROJECT_NAME}}

## Instructions

_Configure agent instructions here._
`
)

// EventEmitter is called for each risky fix that requires manual approval.
type EventEmitter func(event shared.ProvisionEvent)

// Provisioner checks environment requirements and auto-fixes safe items.
type Provisioner struct {
	projectDir  string
	projectName string
	manifest    *config.EnvManifest
	templateDir string
	emitter     EventEmitter
}

// NewProvisioner creates a provisioner for a project directory.
func NewProvisioner(projectDir, projectName string, manifest *config.EnvManifest, templateDir string, emitter EventEmitter) *Provisioner {
	if emitter == nil {
		emitter = func(shared.ProvisionEvent) {}
	}
	return &Provisioner{
		projectDir:  projectDir,
		projectName: projectName,
		manifest:    manifest,
		templateDir: templateDir,
		emitter:     emitter,
	}
}

// Provision runs drift detection and applies safe fixes.
// Risky fixes emit events via the EventEmitter instead of being applied.
func (p *Provisioner) Provision() (*shared.ProvisionResult, error) {
	drift := p.detectDrift()

	result := &shared.ProvisionResult{
		Applied:   []shared.ProvisionAction{},
		Pending:   []shared.ProvisionPending{},
		Timestamp: time.Now().UTC(),
	}

	for _, item := range drift {
		if item.Status == shared.DriftStatusOK {
			continue
		}

		if isSafeCategory(item.Category) {
			action, err := p.applySafeFix(item)
			if err != nil {
				result.Status = shared.ProvisionStatusFailed
				return result, fmt.Errorf("safe fix failed for %s/%s: %w", item.Category, item.Item, err)
			}
			if action != nil {
				result.Applied = append(result.Applied, *action)
			}
		} else {
			pending := p.emitRiskyFix(item)
			result.Pending = append(result.Pending, pending)
		}
	}

	switch {
	case len(result.Pending) > 0 && len(result.Applied) > 0:
		result.Status = shared.ProvisionStatusPartial
	case len(result.Pending) > 0:
		result.Status = shared.ProvisionStatusPartial
	default:
		result.Status = shared.ProvisionStatusCompleted
	}

	return result, nil
}

func isSafeCategory(cat shared.DriftCategory) bool {
	switch cat {
	case shared.DriftCategoryAgentConfig,
		shared.DriftCategoryContext,
		shared.DriftCategoryDocs,
		shared.DriftCategoryGit,
		shared.DriftCategoryEnvVars:
		return true
	default:
		return false
	}
}

func (p *Provisioner) detectDrift() []shared.DriftItem {
	var items []shared.DriftItem

	if p.manifest == nil || p.manifest.Requirements == nil {
		return items
	}
	reqs := p.manifest.Requirements

	// agent_config: check AGENT.md
	if reqs.AgentConfig != nil {
		agentMDPath := filepath.Join(p.projectDir, "AGENT.md")
		if _, err := os.Stat(agentMDPath); os.IsNotExist(err) {
			items = append(items, shared.DriftItem{
				Category: shared.DriftCategoryAgentConfig,
				Item:     "AGENT.md",
				Expected: "exists",
				Actual:   "missing",
				Status:   shared.DriftStatusMissing,
			})
		}
	}

	// context: check files and directories
	if reqs.Context != nil {
		for _, file := range reqs.Context.Files {
			path := filepath.Join(p.projectDir, file)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				items = append(items, shared.DriftItem{
					Category: shared.DriftCategoryContext,
					Item:     file,
					Expected: "exists",
					Actual:   "missing",
					Status:   shared.DriftStatusMissing,
				})
			}
		}
		for _, dir := range reqs.Context.Directories {
			path := filepath.Join(p.projectDir, dir)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				items = append(items, shared.DriftItem{
					Category: shared.DriftCategoryContext,
					Item:     dir,
					Expected: "exists",
					Actual:   "missing",
					Status:   shared.DriftStatusMissing,
				})
			}
		}
	}

	// docs: check required documents
	if reqs.Docs != nil {
		for _, doc := range reqs.Docs.Required {
			path := filepath.Join(p.projectDir, doc)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				items = append(items, shared.DriftItem{
					Category: shared.DriftCategoryDocs,
					Item:     doc,
					Expected: "exists",
					Actual:   "missing",
					Status:   shared.DriftStatusMissing,
				})
			}
		}
	}

	// git: check hooks
	if reqs.Git != nil {
		for _, hook := range reqs.Git.Hooks {
			hookPath := filepath.Join(p.projectDir, ".git", "hooks", hook)
			if _, err := os.Stat(hookPath); os.IsNotExist(err) {
				items = append(items, shared.DriftItem{
					Category: shared.DriftCategoryGit,
					Item:     hook,
					Expected: "exists",
					Actual:   "missing",
					Status:   shared.DriftStatusMissing,
				})
			}
		}
	}

	// env_vars: check required env vars
	if reqs.EnvVars != nil {
		for envVar, status := range reqs.EnvVars {
			if status != "required" {
				continue
			}
			if os.Getenv(envVar) == "" {
				items = append(items, shared.DriftItem{
					Category: shared.DriftCategoryEnvVars,
					Item:     envVar,
					Expected: "set",
					Actual:   "missing",
					Status:   shared.DriftStatusMissing,
				})
			}
		}
	}

	// runtime: check installed runtimes (risky)
	for runtime, version := range reqs.Runtime {
		items = append(items, shared.DriftItem{
			Category: shared.DriftCategoryRuntime,
			Item:     runtime,
			Expected: version,
			Actual:   "missing",
			Status:   shared.DriftStatusMissing,
		})
	}

	// tools: check installed tools (risky)
	for tool, version := range reqs.Tools {
		items = append(items, shared.DriftItem{
			Category: shared.DriftCategoryTools,
			Item:     tool,
			Expected: version,
			Actual:   "missing",
			Status:   shared.DriftStatusMissing,
		})
	}

	return items
}

func (p *Provisioner) applySafeFix(item shared.DriftItem) (*shared.ProvisionAction, error) {
	switch item.Category {
	case shared.DriftCategoryAgentConfig:
		return p.createAgentMD()
	case shared.DriftCategoryContext:
		return p.createContextItem(item.Item)
	case shared.DriftCategoryDocs:
		return p.createDoc(item.Item)
	case shared.DriftCategoryGit:
		return p.installGitHook(item.Item)
	case shared.DriftCategoryEnvVars:
		return p.injectEnvVar(item.Item)
	default:
		return nil, fmt.Errorf("unknown safe category: %s", item.Category)
	}
}

func (p *Provisioner) createAgentMD() (*shared.ProvisionAction, error) {
	target := filepath.Join(p.projectDir, "AGENT.md")

	if _, err := os.Stat(target); err == nil {
		return nil, nil
	}

	content := strings.ReplaceAll(defaultAgentMD, "{{PROJECT_NAME}}", p.projectName)
	if p.templateDir != "" {
		tmplPath := filepath.Join(p.templateDir, "AGENT.md")
		if data, err := os.ReadFile(tmplPath); err == nil {
			content = strings.ReplaceAll(string(data), "{{PROJECT_NAME}}", p.projectName)
		}
	}

	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to create AGENT.md: %w", err)
	}

	return &shared.ProvisionAction{
		Category: shared.DriftCategoryAgentConfig,
		Item:     "AGENT.md",
		Action:   "created",
		Path:     target,
	}, nil
}

func (p *Provisioner) createContextItem(item string) (*shared.ProvisionAction, error) {
	target := filepath.Join(p.projectDir, item)

	if _, err := os.Stat(target); err == nil {
		return nil, nil
	}

	if strings.HasSuffix(item, "/") || strings.HasSuffix(item, string(os.PathSeparator)) {
		if err := os.MkdirAll(target, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", item, err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, fmt.Errorf("failed to create parent for %s: %w", item, err)
		}
		if err := os.WriteFile(target, []byte(""), 0644); err != nil {
			return nil, fmt.Errorf("failed to create file %s: %w", item, err)
		}
	}

	return &shared.ProvisionAction{
		Category: shared.DriftCategoryContext,
		Item:     item,
		Action:   "created",
		Path:     target,
	}, nil
}

func (p *Provisioner) createDoc(docName string) (*shared.ProvisionAction, error) {
	target := filepath.Join(p.projectDir, docName)

	if _, err := os.Stat(target); err == nil {
		return nil, nil
	}

	content := fmt.Sprintf("# %s\n", strings.TrimSuffix(docName, filepath.Ext(docName)))
	if p.templateDir != "" {
		tmplPath := filepath.Join(p.templateDir, docName)
		if data, err := os.ReadFile(tmplPath); err == nil {
			content = string(data)
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent for %s: %w", docName, err)
	}

	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to create doc %s: %w", docName, err)
	}

	return &shared.ProvisionAction{
		Category: shared.DriftCategoryDocs,
		Item:     docName,
		Action:   "created",
		Path:     target,
	}, nil
}

func (p *Provisioner) installGitHook(hookName string) (*shared.ProvisionAction, error) {
	hooksDir := filepath.Join(p.projectDir, ".git", "hooks")
	target := filepath.Join(hooksDir, hookName)

	if _, err := os.Stat(target); err == nil {
		return nil, nil
	}

	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create hooks directory: %w", err)
	}

	content := fmt.Sprintf("#!/bin/sh\n# %s hook - hal-o-swarm managed\nexit 0\n", hookName)
	if p.templateDir != "" {
		tmplPath := filepath.Join(p.templateDir, "hooks", hookName)
		if data, err := os.ReadFile(tmplPath); err == nil {
			content = string(data)
		}
	}

	if err := os.WriteFile(target, []byte(content), 0755); err != nil {
		return nil, fmt.Errorf("failed to install hook %s: %w", hookName, err)
	}

	return &shared.ProvisionAction{
		Category: shared.DriftCategoryGit,
		Item:     hookName,
		Action:   "created",
		Path:     target,
	}, nil
}

func (p *Provisioner) injectEnvVar(envVar string) (*shared.ProvisionAction, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	rcFile := filepath.Join(home, ".bashrc")
	if shell := os.Getenv("SHELL"); strings.Contains(shell, "zsh") {
		rcFile = filepath.Join(home, ".zshrc")
	}

	marker := fmt.Sprintf("%s %s", envMarkerComment, envVar)
	if data, err := os.ReadFile(rcFile); err == nil {
		if strings.Contains(string(data), marker) {
			return nil, nil
		}
	}

	line := fmt.Sprintf("\n%s\nexport %s=\n", marker, envVar)

	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", rcFile, err)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		return nil, fmt.Errorf("failed to write to %s: %w", rcFile, err)
	}

	return &shared.ProvisionAction{
		Category: shared.DriftCategoryEnvVars,
		Item:     envVar,
		Action:   "injected",
		Path:     rcFile,
	}, nil
}

func (p *Provisioner) emitRiskyFix(item shared.DriftItem) shared.ProvisionPending {
	cmd := suggestedCommand(item)
	token := generateApprovalToken()

	p.emitter(shared.ProvisionEvent{
		Type: "provision.manual_required",
		Data: shared.ProvisionEventData{
			Category:      item.Category,
			Item:          item.Item,
			Expected:      item.Expected,
			Actual:        item.Actual,
			SuggestedCmd:  cmd,
			ApprovalToken: token,
		},
	})

	return shared.ProvisionPending{
		Category: item.Category,
		Item:     item.Item,
		Reason:   "manual_approval_required",
		Command:  cmd,
	}
}

func suggestedCommand(item shared.DriftItem) string {
	switch item.Category {
	case shared.DriftCategoryRuntime:
		switch item.Item {
		case "node":
			return "apt-get install nodejs"
		case "python":
			return "apt-get install python3"
		case "java":
			return "apt-get install openjdk-17-jdk"
		default:
			return fmt.Sprintf("apt-get install %s", item.Item)
		}
	case shared.DriftCategoryTools:
		return fmt.Sprintf("apt-get install %s", item.Item)
	default:
		return fmt.Sprintf("install %s", item.Item)
	}
}

func generateApprovalToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "prov_fallback"
	}
	return "prov_" + hex.EncodeToString(b)
}
