package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractSnippetPin covers the parser's three branches: a snippet
// written by writeShellSnippet (the happy path), a snippet missing the
// pin (regression: v1-shaped snippet or a hand edit), and a non-existent
// snippet (covered by checkShellSnippet upstream).
func TestExtractSnippetPin(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		dir := t.TempDir()
		if err := writeShellSnippet(dir, nil, nil); err != nil {
			t.Fatalf("write: %v", err)
		}
		pin := extractSnippetPin(dir)
		if pin == "" {
			t.Fatal("extractSnippetPin returned empty for a freshly-generated snippet")
		}
		// The pin must point at the running test binary, which is what
		// writeShellSnippet embedded via os.Executable().
		exe, err := os.Executable()
		if err != nil {
			t.Fatalf("os.Executable: %v", err)
		}
		if !samePath(pin, exe) {
			t.Errorf("pin = %q, want path equivalent to %q", pin, exe)
		}
	})

	t.Run("missing pin line", func(t *testing.T) {
		dir := t.TempDir()
		// Drop a snippet that does not declare the pin (older shape).
		if err := os.MkdirAll(filepath.Join(dir, "shell"), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "# stale snippet\nfunction global:o { }"
		if err := os.WriteFile(shellPath(dir), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if pin := extractSnippetPin(dir); pin != "" {
			t.Errorf("pin = %q, want empty for snippet without $global:onixExe", pin)
		}
	})

	t.Run("missing snippet", func(t *testing.T) {
		dir := t.TempDir()
		if pin := extractSnippetPin(dir); pin != "" {
			t.Errorf("pin = %q, want empty when snippet file is absent", pin)
		}
	})
}

// TestCheckSnippetPin walks the doctor check itself through its three
// outcomes: ok, warn (no pin), warn (pinned path missing).
func TestCheckSnippetPin(t *testing.T) {
	t.Run("ok when snippet is fresh", func(t *testing.T) {
		dir := t.TempDir()
		if err := writeShellSnippet(dir, nil, nil); err != nil {
			t.Fatal(err)
		}
		r := checkSnippetPin(dir)
		if r.status != "ok" {
			t.Errorf("status = %q, want ok (detail=%s)", r.status, r.detail)
		}
	})

	t.Run("skipped when snippet missing", func(t *testing.T) {
		dir := t.TempDir()
		r := checkSnippetPin(dir)
		if r.name != "" {
			t.Errorf("expected zero-value result when snippet absent, got %+v", r)
		}
	})

	t.Run("warn when pin points to missing file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "shell"), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "$global:onixExe = 'C:\\nope\\does-not-exist.exe'\n"
		if err := os.WriteFile(shellPath(dir), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		r := checkSnippetPin(dir)
		if r.status != "warn" {
			t.Errorf("status = %q, want warn (detail=%s)", r.status, r.detail)
		}
		if !strings.Contains(r.detail, "missing") {
			t.Errorf("detail = %q, want it to mention 'missing'", r.detail)
		}
	})
}
