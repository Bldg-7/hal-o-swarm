package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStatusCommandUsesConfiguredPath(t *testing.T) {
	configured := "/custom/bin/opencode"
	command := resolveStatusCommand(ToolOpencode, configured, nil)

	if len(command) == 0 {
		t.Fatal("expected status command")
	}
	if command[0] != configured {
		t.Fatalf("expected configured binary %q, got %q", configured, command[0])
	}
}

func TestResolveStatusCommandUsesFallbackPath(t *testing.T) {
	t.Setenv("PATH", "")

	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "opencode")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	original := toolBinaryFallbacks[ToolOpencode]
	toolBinaryFallbacks[ToolOpencode] = []string{binaryPath}
	t.Cleanup(func() {
		toolBinaryFallbacks[ToolOpencode] = original
	})

	command := resolveStatusCommand(ToolOpencode, "", nil)
	if len(command) == 0 {
		t.Fatal("expected status command")
	}
	if command[0] != binaryPath {
		t.Fatalf("expected fallback binary %q, got %q", binaryPath, command[0])
	}
}

func TestIsExecutableFile(t *testing.T) {
	tempDir := t.TempDir()
	execPath := filepath.Join(tempDir, "exec-tool")
	nonExecPath := filepath.Join(tempDir, "nonexec-tool")

	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write exec file: %v", err)
	}
	if err := os.WriteFile(nonExecPath, []byte("not executable\n"), 0o644); err != nil {
		t.Fatalf("write non-exec file: %v", err)
	}

	if !isExecutableFile(execPath) {
		t.Fatalf("expected executable file to be detected: %s", execPath)
	}
	if isExecutableFile(nonExecPath) {
		t.Fatalf("expected non-executable file to be rejected: %s", nonExecPath)
	}
	if isExecutableFile(filepath.Join(tempDir, "missing")) {
		t.Fatal("expected missing file to be rejected")
	}
}
