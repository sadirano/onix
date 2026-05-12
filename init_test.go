package main

import (
	"os"
	"strings"
	"testing"
)

// TestWriteShellSnippet_NoActions confirms the snippet contains all the
// built-in functions and the completer registration even when no custom
// actions or plugins are declared. This is the first-run shape.
func TestWriteShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := writeShellSnippet(dir, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(shellPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"$global:onixExe",                // pinned binary path declaration
		"$onixAliasCompleter",            // completer script block
		"& $global:onixExe list-names",   // completer calls the pinned binary
		"function global:o {",            // built-in functions present
		"function global:n {",
		"function global:s {",
		"function global:y {",
		"function global:r {",
		"& $global:onixExe resolve",      // o uses the pinned binary
		"Register-ArgumentCompleter -CommandName o,n,s,y,r -ParameterName Alias",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("snippet missing %q", want)
		}
	}
}

// TestWriteShellSnippet_WithActions verifies that custom action functions
// land in the snippet and that the completer registration includes them.
// The action's exec value should appear in the generated function body so
// it dispatches through `onix exec <name>`.
func TestWriteShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := writeShellSnippet(dir, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(shellPath(dir))
	got := string(data)
	for _, want := range []string{
		"function global:test {",
		"& $global:onixExe exec test $Alias",
		"function global:pr {",
		"& $global:onixExe exec pr $Alias",
		"Register-ArgumentCompleter -CommandName o,n,s,y,r,test,pr -ParameterName Alias",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("snippet missing %q\n--- snippet ---\n%s", want, got)
		}
	}
}

// TestWriteShellSnippet_WithPlugins locks the wrapper-generation contract
// for plugins: a single-entry plugin emits one wrapper named after the
// plugin; a multi-entry plugin additionally emits one wrapper per entry,
// each calling plugin-exec with the right pluginName + entryName tokens.
// All names participate in the completer registration.
func TestWriteShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plugins := []Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"}, // EffectiveCmd == "stop"
			}},
	}
	if err := writeShellSnippet(dir, nil, plugins); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(shellPath(dir))
	got := string(data)

	wants := []string{
		// Single-entry plugin: one wrapper, empty entry token.
		`function global:tts {`,
		`& $global:onixExe plugin-exec tts "" $Alias`,

		// Multi-entry plugin: main wrapper.
		`function global:timer {`,
		`& $global:onixExe plugin-exec timer "" $Alias`,

		// Entry wrapper: function named t-start (per `cmd` override),
		// passes pluginName=timer and entryName=start.
		`function global:t-start {`,
		`& $global:onixExe plugin-exec timer "start" $Alias`,

		// Entry without cmd override falls back to entry name.
		`function global:stop {`,
		`& $global:onixExe plugin-exec timer "stop" $Alias`,

		// Completer registration covers all of them, in declaration order.
		`Register-ArgumentCompleter -CommandName o,n,s,y,r,tts,timer,t-start,stop -ParameterName Alias`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("snippet missing %q\n--- snippet ---\n%s", want, got)
		}
	}
}
