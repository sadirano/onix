package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/snippet"
)

// snippetPathForOS returns the snippet file the host platform actually
// reads/writes. The tests drive the public extractSnippetPin /
// checkSnippetPin entry points, which now branch on runtime.GOOS, so the
// test setup has to write whichever file those functions will look at.
func snippetPathForOS(home string) string {
	if runtime.GOOS == "windows" {
		return snippet.PwshPath(home)
	}
	return snippet.BashPath(home)
}

// staleSnippetBody returns a snippet body in the host platform's format
// that does NOT contain the pin line, used by the "missing pin" test.
func staleSnippetBody() string {
	if runtime.GOOS == "windows" {
		return "# stale snippet\nfunction global:o { }\n"
	}
	return "# stale snippet\no() { :; }\n"
}

// missingPinBody returns a snippet body in the host platform's format with
// the pin line pointing at a path that definitely does not exist.
func missingPinBody() string {
	if runtime.GOOS == "windows" {
		return "$global:onixExe = 'C:\\nope\\does-not-exist.exe'\n"
	}
	return "export ONIX_EXE='/nope/does-not-exist'\n"
}

// TestExtractSnippetPin covers the parser's three branches: a snippet
// written by writeShellSnippet (the happy path), a snippet missing the
// pin (regression: v1-shaped snippet or a hand edit), and a non-existent
// snippet (covered by checkShellSnippet upstream).
func TestExtractSnippetPin(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		dir := t.TempDir()
		if err := snippet.WriteShellSnippet(dir, nil, nil, nil); err != nil {
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
		if err := os.WriteFile(snippetPathForOS(dir), []byte(staleSnippetBody()), 0o644); err != nil {
			t.Fatal(err)
		}
		if pin := extractSnippetPin(dir); pin != "" {
			t.Errorf("pin = %q, want empty for snippet without pin line", pin)
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
		if err := snippet.WriteShellSnippet(dir, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		r := checkSnippetPin(dir)
		if r.Status != "ok" {
			t.Errorf("Status = %q, want ok (Detail=%s)", r.Status, r.Detail)
		}
	})

	t.Run("skipped when snippet missing", func(t *testing.T) {
		dir := t.TempDir()
		r := checkSnippetPin(dir)
		if r.Name != "" {
			t.Errorf("expected zero-value result when snippet absent, got %+v", r)
		}
	})

	t.Run("warn when pin points to missing file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "shell"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(snippetPathForOS(dir), []byte(missingPinBody()), 0o644); err != nil {
			t.Fatal(err)
		}
		r := checkSnippetPin(dir)
		if r.Status != "warn" {
			t.Errorf("status = %q, want warn (detail=%s)", r.Status, r.Detail)
		}
		if !strings.Contains(r.Detail, "missing") {
			t.Errorf("detail = %q, want it to mention 'missing'", r.Detail)
		}
	})
}

// TestCheckBashLikeProfile exercises the three branches of the Linux
// shell-profile check: no rc files found, rc file present but doesn't
// source the snippet, rc file sources the snippet.
//
// We point USERPROFILE/HOME at a tempdir so the real user's rc files
// never participate. On Windows the function still reads $HOME via
// os.UserHomeDir; the t.Setenv calls cover both variants.
func TestCheckBashLikeProfile(t *testing.T) {
	t.Run("warn when no rc files", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		r := checkBashLikeProfile(home)
		if r.Status != "warn" {
			t.Errorf("status = %q, want warn (no rc files)", r.Status)
		}
		if !strings.Contains(r.Detail, "neither") {
			t.Errorf("detail = %q, want it to mention missing rc files", r.Detail)
		}
	})

	t.Run("warn when rc files exist but don't source", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# unrelated rc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := checkBashLikeProfile(home)
		if r.Status != "warn" {
			t.Errorf("status = %q, want warn (not sourced)", r.Status)
		}
		if !strings.Contains(r.Detail, "onix init") {
			t.Errorf("detail = %q, want it to suggest 'onix init'", r.Detail)
		}
	})

	t.Run("ok when rc sources the snippet", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		// .zshrc references the absolute path that snippet.BashPath would return.
		body := "# onix\n. '" + snippet.BashPath(home) + "'\n"
		if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		r := checkBashLikeProfile(home)
		if r.Status != "ok" {
			t.Errorf("status = %q (detail=%s), want ok", r.Status, r.Detail)
		}
	})
}
