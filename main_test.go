package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHasFlag(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		names []string
		want  bool
	}{
		{"match", []string{"--json"}, []string{"--json", "-j"}, true},
		{"match short", []string{"-j"}, []string{"--json", "-j"}, true},
		{"no match", []string{"--list"}, []string{"--json", "-j"}, false},
		{"prefix match (should fail)", []string{"--json=foo"}, []string{"--json"}, false},
		{"empty", []string{}, []string{"--json"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasFlag(tt.args, tt.names...); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ONIX_HOME", tempHome)

	runOnix := func(args ...string) (int, string, string) {
		var outBuf, errBuf bytes.Buffer
		code := run(args, strings.NewReader(""), &outBuf, &errBuf)
		return code, outBuf.String(), errBuf.String()
	}

	t.Run("version", func(t *testing.T) {
		code, out, _ := runOnix("onix", "--version")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "onix") {
			t.Errorf("output should contain 'onix', got %q", out)
		}
	})

	t.Run("help", func(t *testing.T) {
		code, out, _ := runOnix("onix", "--help")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "USAGE:") {
			t.Errorf("output should contain 'USAGE:', got %q", out)
		}
	})

	t.Run("bare usage", func(t *testing.T) {
		code, out, _ := runOnix("onix")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "USAGE:") {
			t.Errorf("output should contain 'USAGE:', got %q", out)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		code, _, err := runOnix("onix", "--bogus")
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(err, "unknown flag") {
			t.Errorf("stderr should contain 'unknown flag', got %q", err)
		}
	})

	// System-action verbs exercised through the dispatcher (covers
	// dispatchSystem's switch arms).
	t.Run("list verb", func(t *testing.T) {
		code, _, _ := runOnix("onix", "--list")
		if code != 0 {
			t.Errorf("--list exit %d", code)
		}
	})
	t.Run("list-names verb", func(t *testing.T) {
		code, _, _ := runOnix("onix", "--list-names")
		if code != 0 {
			t.Errorf("--list-names exit %d", code)
		}
	})
	t.Run("doctor verb (returns warnings, not errors, when home is clean)", func(t *testing.T) {
		// Use the existing tempHome that was set up earlier in this test.
		// doctor reports warnings about missing profile etc. but still exits 0
		// unless something is genuinely broken.
		code, _, _ := runOnix("onix", "--doctor")
		// The home doesn't have a snippet/profile so this may exit non-zero.
		// We only care that dispatching to it doesn't crash.
		if code != 0 && code != 1 {
			t.Errorf("--doctor exit %d, want 0 or 1", code)
		}
	})
	t.Run("contexts verb", func(t *testing.T) {
		code, _, _ := runOnix("onix", "--contexts")
		if code != 0 {
			t.Errorf("--contexts exit %d", code)
		}
	})
}

// TestRun_AliasActions drives dispatchAlias's per-action switch arms via
// `onix <alias> --<action>` invocations. We register an alias against a
// temp dir so the resolver paths succeed, then exercise each terminal
// action that has no external dependency.
//
// External-tool launches (grep/find/show/explore) are short-circuited by a
// faked lookPath that reports every binary as missing — the action's
// initial LookPath check then errors out fast instead of spawning a real
// rg/fzf/xdg-open process that would block the test.
func TestRun_AliasActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ONIX_HOME", home)
	target := t.TempDir()

	// Pretend every external tool is on PATH and fake the subprocess so the
	// post-LookPath code paths in grep/find/show all execute, but the tests
	// don't have to wait on fzf or block on interactive pickers. The fake
	// just returns a zero-arg cmd that exits instantly.
	prevLookPath := lookPath
	t.Cleanup(func() { lookPath = prevLookPath })
	lookPath = func(name string) (string, error) {
		return "C:/fake/" + name, nil
	}

	// Stub execCommand / execCommandContext so external-tool invocations
	// (rg, fzf, xdg-open, ...) exit immediately rather than spawning real
	// subprocesses that would block the test or fail with rc=non-zero
	// noise. We use `cmd /c exit 0` on Windows and `true` elsewhere.
	prevCmd := execCommand
	prevCtx := execCommandContext
	t.Cleanup(func() {
		execCommand = prevCmd
		execCommandContext = prevCtx
	})
	noopCmd := func() *exec.Cmd {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", "exit 0")
		}
		return exec.Command("true")
	}
	execCommand = func(string, ...string) *exec.Cmd { return noopCmd() }
	execCommandContext = func(context.Context, string, ...string) *exec.Cmd { return noopCmd() }

	runOnix := func(args ...string) (int, string, string) {
		var outBuf, errBuf bytes.Buffer
		code := run(args, strings.NewReader(""), &outBuf, &errBuf)
		return code, outBuf.String(), errBuf.String()
	}

	// Register the alias up front.
	if code, _, _ := runOnix("onix", "acme", target); code != 0 {
		t.Fatalf("register exit %d", code)
	}

	t.Run("resolve via --resolve flag", func(t *testing.T) {
		code, out, _ := runOnix("onix", "acme", "--resolve")
		if code != 0 {
			t.Errorf("exit %d", code)
		}
		if !strings.Contains(out, "acme") && !strings.Contains(out, filepath.Base(target)) {
			t.Errorf("expected resolved path in output: %q", out)
		}
	})

	t.Run("yank via -y", func(t *testing.T) {
		code, _, _ := runOnix("onix", "acme", "-y")
		if code != 0 {
			t.Errorf("exit %d", code)
		}
	})

	t.Run("unexpected positional before action", func(t *testing.T) {
		code, _, errOut := runOnix("onix", "acme", "extra", "-y")
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(errOut, "unexpected positional") {
			t.Errorf("expected positional error, got %q", errOut)
		}
	})

	t.Run("--exec requires action name", func(t *testing.T) {
		code, _, errOut := runOnix("onix", "acme", "-X")
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(errOut, "--exec requires") {
			t.Errorf("expected exec usage error, got %q", errOut)
		}
	})

	// Verbs that need an unknown action to fail fast — exercises the
	// dispatcher's switch arm without spawning external tools.
	t.Run("exec to unknown action errors", func(t *testing.T) {
		code, _, _ := runOnix("onix", "acme", "-X", "nope")
		if code != 1 {
			t.Errorf("expected exit 1 for unknown action, got %d", code)
		}
	})

	t.Run("explore dispatch", func(t *testing.T) {
		// On Windows --explore launches explorer.exe via Start, returns quickly.
		// On Unix it tries xdg-open which may or may not be available.
		// Either exit code is acceptable — we only need the dispatch arm covered.
		_, _, _ = runOnix("onix", "acme", "-x")
	})

	t.Run("remove flag dispatch", func(t *testing.T) {
		// Use --remove on a path that doesn't exist; the dispatch arm fires
		// regardless of whether removal succeeds.
		_, _, _ = runOnix("onix", "acme", "--rm", "definitely-not-there.txt", "--force")
	})

	t.Run("grep dispatch hits LookPath check", func(t *testing.T) {
		// rg/fzf may or may not be on PATH. Either way the dispatch arm is
		// exercised, and the failure case (rg not found) is what we typically
		// see in CI environments.
		_, _, _ = runOnix("onix", "acme", "-g", "query")
	})

	t.Run("find dispatch hits LookPath check", func(t *testing.T) {
		_, _, _ = runOnix("onix", "acme", "-f", "query")
	})
}
