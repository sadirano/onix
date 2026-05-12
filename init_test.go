package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files")


// The shell-snippet tests drive the underlying platform-specific writers
// (writePwshShellSnippet, writeBashShellSnippet) directly rather than
// going through writeShellSnippet, which now branches on runtime.GOOS.
// This way the PowerShell snippet shape is verified on Linux CI too and
// vice versa — the host platform doesn't determine which snippet we test.

// TestWritePwshShellSnippet_NoActions confirms the snippet contains all
// the built-in functions and the completer registration even when no
// custom actions or plugins are declared. This is the first-run shape.
func TestWritePwshShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := writePwshShellSnippet(dir, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(shellPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, string(data), "pwsh-no-actions.ps1.golden")
}

// TestWritePwshShellSnippet_WithActions verifies that custom action
// functions land in the snippet and that the completer registration
// includes them. The action's name should appear in the generated function
// body so it dispatches through `onix exec <name>`.
func TestWritePwshShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := writePwshShellSnippet(dir, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(shellPath(dir))
	assertGolden(t, string(data), "pwsh-with-actions.ps1.golden")
}

// TestWritePwshShellSnippet_WithPlugins locks the wrapper-generation
// contract for plugins: a single-entry plugin emits one wrapper named
// after the plugin; a multi-entry plugin additionally emits one wrapper
// per entry, each calling plugin-exec with the right pluginName +
// entryName tokens. All names participate in the completer registration.
func TestWritePwshShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plugins := []Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"}, // EffectiveCmd == "stop"
			}},
	}
	if err := writePwshShellSnippet(dir, nil, plugins); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(shellPath(dir))
	assertGolden(t, string(data), "pwsh-with-plugins.ps1.golden")
}

// TestWriteBashShellSnippet_NoActions mirrors the PowerShell "no actions"
// test for the bash/zsh path: pin line, built-in functions, completer
// hooks, and the `complete -F`/`compdef` registration.
func TestWriteBashShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := writeBashShellSnippet(dir, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(bashShellPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, string(data), "bash-no-actions.sh.golden")
}

// TestWriteBashShellSnippet_WithActions verifies custom action wrappers
// and their inclusion in the completer registration.
func TestWriteBashShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := writeBashShellSnippet(dir, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(bashShellPath(dir))
	assertGolden(t, string(data), "bash-with-actions.sh.golden")
}
// TestWriteBashShellSnippet_WithPlugins mirrors the plugin-wrapper
// contract verified for the PowerShell snippet, in the bash dialect.
func TestWriteBashShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plugins := []Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"},
			}},
	}
	if err := writeBashShellSnippet(dir, nil, plugins); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(bashShellPath(dir))
	assertGolden(t, string(data), "bash-with-plugins.sh.golden")
}

// TestWriteShellSnippet_HostPlatformOnly confirms the dispatcher writes
// exactly one snippet on disk — the one for the host platform — and not
// the other. Previously it wrote both unconditionally, which led to a
// stale .ps1 file on Linux pointing at an ELF binary.
func TestWriteShellSnippet_HostPlatformOnly(t *testing.T) {
	dir := t.TempDir()
	if err := writeShellSnippet(dir, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	// One should exist, the other should not.
	pwshExists := fileExists(shellPath(dir))
	bashExists := fileExists(bashShellPath(dir))
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
	path := filepath.Join("testdata", "snippet", goldenName)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create)", err)
	}
	want := string(data)
	if got != want {
		t.Errorf("golden mismatch for %s\nRun 'go test ./... -update' to accept the new output if correct.", goldenName)
	}
}

