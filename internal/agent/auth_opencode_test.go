package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
)

func TestOpencodeAuthBinaryMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	adapter := NewOpencodeAuthAdapterWithCommand(nil, nil, []string{"missing-opencode-binary", "auth", "list"})
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusNotInstalled {
		t.Fatalf("status = %q, want %q", report.Status, shared.AuthStatusNotInstalled)
	}
}

func TestOpencodeAuthNoAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	binary := writeExecutableOpencode(t, filepath.Join(t.TempDir(), "opencode"))
	adapter := NewOpencodeAuthAdapterWithCommand(nil, nil, []string{binary, "auth", "list"})
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusUnauthenticated {
		t.Fatalf("status = %q, want %q", report.Status, shared.AuthStatusUnauthenticated)
	}
}

func TestOpencodeAuthParsesCredentialFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	writeAuthFixtureFile(t, authPath, `{"credentials":[{"provider":"openai","type":"oauth"},{"provider":"anthropic","type":"api"}]}`)

	binary := writeExecutableOpencode(t, filepath.Join(t.TempDir(), "opencode"))
	adapter := NewOpencodeAuthAdapterWithCommand(nil, nil, []string{binary, "auth", "list"})
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusAuthenticated {
		t.Fatalf("status = %q, want %q", report.Status, shared.AuthStatusAuthenticated)
	}
}

func TestOpencodeAuthUsesXDGDataHome(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", xdg)

	authPath := filepath.Join(xdg, "opencode", "auth.json")
	writeAuthFixtureFile(t, authPath, `{"providers":{"openai":{"token":"x"}}}`)

	binary := writeExecutableOpencode(t, filepath.Join(t.TempDir(), "opencode"))
	adapter := NewOpencodeAuthAdapterWithCommand(nil, nil, []string{binary, "auth", "list"})
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusAuthenticated {
		t.Fatalf("status = %q, want %q", report.Status, shared.AuthStatusAuthenticated)
	}
}

func TestOpencodeAuthInvalidFileFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	writeAuthFixtureFile(t, authPath, `not-json`)

	binary := writeExecutableOpencode(t, filepath.Join(t.TempDir(), "opencode"))
	adapter := NewOpencodeAuthAdapterWithCommand(nil, nil, []string{binary, "auth", "list"})
	report := adapter.CheckAuth(context.Background())

	if report.Status != shared.AuthStatusError {
		t.Fatalf("status = %q, want %q", report.Status, shared.AuthStatusError)
	}
	if report.Reason != "invalid auth file format" {
		t.Fatalf("reason = %q, want invalid auth file format", report.Reason)
	}
}

func writeExecutableOpencode(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
	return path
}

func writeAuthFixtureFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
