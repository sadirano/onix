package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/plugins"
	"github.com/sadirano/onix/internal/snippet"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// TestWritePwshShellSnippet_NoActions confirms the snippet contains all
// the built-in functions.
func TestWritePwshShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := snippet.WritePwshShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(snippet.PwshPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, scrub(string(data), dir),
		"pwsh-no-actions.ps1.golden")
}

// TestWritePwshShellSnippet_WithActions verifies custom action functions.
func TestWritePwshShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []config.Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := snippet.WritePwshShellSnippet(dir, nil, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(snippet.PwshPath(dir))
	assertGolden(t, scrub(string(data), dir),
		"pwsh-with-actions.ps1.golden")
}

// TestWritePwshShellSnippet_WithPlugins locks the wrapper-generation contract.
func TestWritePwshShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plgs := []plugins.Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{
			Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []plugins.PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"},
			},
		},
	}
	if err := snippet.WritePwshShellSnippet(dir, nil, nil, plgs); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(snippet.PwshPath(dir))
	assertGolden(t, scrub(string(data), dir),
		"pwsh-with-plugins.ps1.golden")
}

// TestWriteBashShellSnippet_NoActions mirrors the PowerShell "no actions" test.
func TestWriteBashShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := snippet.WriteBashShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(snippet.BashPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, scrub(string(data), dir),
		"bash-no-actions.sh.golden")
}

// TestWriteBashShellSnippet_WithActions verifies custom action wrappers.
func TestWriteBashShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []config.Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := snippet.WriteBashShellSnippet(dir, nil, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(snippet.BashPath(dir))
	assertGolden(t, scrub(string(data), dir),
		"bash-with-actions.sh.golden")
}

// TestWriteBashShellSnippet_WithPlugins mirrors the plugin-wrapper contract.
func TestWriteBashShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plgs := []plugins.Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{
			Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []plugins.PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"},
			},
		},
	}
	if err := snippet.WriteBashShellSnippet(dir, nil, nil, plgs); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(snippet.BashPath(dir))
	assertGolden(t, scrub(string(data), dir),
		"bash-with-plugins.sh.golden")
}

// TestWriteShellSnippet_HostPlatformOnly confirms exactly one snippet on disk.
func TestWriteShellSnippet_HostPlatformOnly(t *testing.T) {
	dir := t.TempDir()
	if err := snippet.WriteShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	pwshExists := fileExists(snippet.PwshPath(dir))
	bashExists := fileExists(snippet.BashPath(dir))
	if pwshExists == bashExists {
		t.Fatalf("expected exactly one snippet, got pwsh=%v bash=%v", pwshExists, bashExists)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func assertGolden(t *testing.T, got, goldenName string) {
	t.Helper()

	// Normalize line endings to LF for cross-platform comparison
	normalize := func(s string) string {
		return strings.ReplaceAll(s, "\r\n", "\n")
	}

	path := filepath.Join("testdata", "snippet", goldenName)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(normalize(got)), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create)", err)
	}
	want := string(data)
	if normalize(got) != normalize(want) {
		t.Errorf("golden mismatch for %s\nGOT:\n%s\nWANT:\n%s\nRun 'go test ./... -update' to accept the new output if correct.", goldenName, got, want)
	}
}

// scrub deterministic paths that change every test run (like t.TempDir())
func scrub(s, tempDir string) string {
	s = strings.ReplaceAll(s, tempDir, "/ONIX_HOME")
	exe, _ := os.Executable()
	if exe != "" {
		s = strings.ReplaceAll(s, exe, "/ONIX_EXE")
	}
	return filepath.ToSlash(s)
}
