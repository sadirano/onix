package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/store"
)

// TestAddCmd_OutputContract locks the stdout/stderr split that the `o`
// shell wrapper depends on:
//
//   - stdout: only the absolute resolved path, one line. This is what the
//     shell function captures and passes to `cd`.
//   - stderr: the human-readable "registered <alias> -> <path>" message.
//
// Drift here would break `o foo C:\path` end-to-end (the shell would cd
// into a directory whose name is "registered foo -> C:\path").
func TestAddCmd_OutputContract(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := captureStdio(func() error {
		return (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatalf("AddCmd.Run: %v", err)
	}

	// stdout: exactly one line, the absolute path, no decoration.
	stdoutLines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(stdoutLines) != 1 {
		t.Fatalf("stdout = %q, want exactly one line", stdout)
	}
	if !samePath(stdoutLines[0], target) {
		t.Errorf("stdout = %q, want absolute path of %q", stdoutLines[0], target)
	}

	// stderr: confirms the registration in human-readable form.
	if !strings.Contains(stderr, "registered acme") {
		t.Errorf("stderr = %q, want a 'registered acme' line", stderr)
	}
}

// TestAddCmd_AutoCreatesDir confirms that registering an alias for a path
// that doesn't exist creates the directory. This is what makes
// `o newalias /new/path` work as a single command.
func TestAddCmd_AutoCreatesDir(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	target := filepath.Join(dir, "does", "not", "exist", "yet")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := captureStdio(func() error {
		return (&AddCmd{Alias: "newdir", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatalf("AddCmd.Run: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("target dir was not created: %v", err)
	}
}

// TestAddCmd_RejectsInvalidName guards the ValidateAliasName plumbing —
// if a future refactor drops the validation call, the shell wrappers
// could write a TOML key that the parser can't read back.
func TestAddCmd_RejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := []string{"foo@bar", "foo/bar", "foo bar", ""}
	for _, name := range bad {
		_, _, err := captureStdio(func() error {
			return (&AddCmd{Alias: name, Path: home}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err == nil {
			t.Errorf("AddCmd with name %q should have errored", name)
		}
	}
}

func TestAddCmd_Metadata(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Initial add with metadata.
	cmd1 := &AddCmd{
		Alias:       "meta",
		Path:        target,
		Description: "A test project",
		Owner:       "dev-team",
		Tags:        []string{"work", "go"},
	}
	if err := cmd1.Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}); err != nil {
		t.Fatalf("Initial AddCmd failed: %v", err)
	}

	// Verify persistence.
	s1, _ := store.LoadStore(home)
	a1, ok := s1.Lookup("meta")
	if !ok {
		t.Fatal("alias 'meta' not found in store")
	}
	if a1.Description != "A test project" || a1.Owner != "dev-team" || len(a1.Tags) != 2 {
		t.Errorf("Metadata not correctly saved: %+v", a1)
	}

	// 2. Update only metadata (merge behavior).
	cmd2 := &AddCmd{
		Alias: "meta",
		Path:  target,
		Owner: "ops-team",
	}
	if err := cmd2.Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}); err != nil {
		t.Fatalf("Update AddCmd failed: %v", err)
	}

	// Verify merge.
	s2, _ := store.LoadStore(home)
	a2, _ := s2.Lookup("meta")
	if a2.Owner != "ops-team" {
		t.Errorf("Owner not updated, got %q", a2.Owner)
	}
	if a2.Description != "A test project" {
		t.Errorf("Description lost during merge, got %q", a2.Description)
	}
	if len(a2.Tags) != 2 {
		t.Errorf("Tags lost during merge, got %v", a2.Tags)
	}
}

// TestListCmd verifies that aliases are listed correctly in both table and JSON modes.
func TestListCmd(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	// Register two aliases.
	pathA, _ := filepath.Abs("a")
	pathB, _ := filepath.Abs("b")
	_ = (&AddCmd{Alias: "a", Path: pathA}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	_ = (&AddCmd{Alias: "b", Path: pathB}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	t.Run("table output", func(t *testing.T) {
		stdout, _, err := captureStdio(func() error {
			return (&ListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, "ALIAS") || !strings.Contains(stdout, "PATH") {
			t.Errorf("table header missing: %q", stdout)
		}
		if !strings.Contains(stdout, "a") || !strings.Contains(stdout, "b") {
			t.Errorf("aliases missing from output: %q", stdout)
		}
	})

	t.Run("JSON output", func(t *testing.T) {
		stdout, _, err := captureStdio(func() error {
			return (&ListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, JSON: true})
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(stdout, "[") {
			t.Errorf("expected JSON array, got: %q", stdout)
		}
		if !strings.Contains(stdout, `"name": "a"`) || !strings.Contains(stdout, fmt.Sprintf(`"path": "%s"`, filepath.ToSlash(pathA))) {
			t.Errorf("JSON output missing data: %q", stdout)
		}
	})
}

// TestRemoveCmd confirms that aliases can be removed.
func TestRemoveCmd(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	_ = (&AddCmd{Alias: "acme", Path: "C:/acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	t.Run("remove existing", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RemoveCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err != nil {
			t.Fatalf("RemoveCmd.Run: %v", err)
		}
		// Confirm it's gone from List.
		stdout, _, _ := captureStdio(func() error {
			return (&ListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if strings.Contains(stdout, "acme") {
			t.Error("alias still present after removal")
		}
	})

	t.Run("remove missing", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RemoveCmd{Alias: "nope"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err == nil {
			t.Error("RemoveCmd on missing alias should have errored")
		}
	})
}

// noopExec returns a (binary, args) pair that exits 0 on the current host.
func noopExec() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "rem"}
	}
	return "true", nil
}

func TestRunCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	bin, args := noopExec()

	t.Run("happy path", func(t *testing.T) {
		argv := append([]string{"acme", bin}, args...)
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: argv}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err != nil {
			t.Errorf("RunCmd.Run: %v", err)
		}
	})

	t.Run("strips leading double-dash", func(t *testing.T) {
		argv := append([]string{"acme", "--", bin}, args...)
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: argv}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err != nil {
			t.Errorf("RunCmd.Run with -- separator: %v", err)
		}
	})

	t.Run("rejects too few args", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: []string{"acme"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err == nil {
			t.Error("RunCmd with only alias should error")
		}
	})

	t.Run("rejects empty argv after --", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: []string{"acme", "--"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err == nil {
			t.Error("RunCmd with bare -- should error")
		}
	})
}

func TestExecCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	bin, args := noopExec()
	// Write config.toml declaring a 'noop' action that runs our no-op binary.
	cfgBody := "[[actions]]\nname = \"noop\"\nexec = \"" + bin + "\"\nargs = ["
	for i, a := range args {
		if i > 0 {
			cfgBody += ", "
		}
		cfgBody += "\"" + a + "\""
	}
	cfgBody += "]\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("happy path", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&ExecCmd{Args: []string{"noop", "acme"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err != nil {
			t.Errorf("ExecCmd.Run: %v", err)
		}
	})

	t.Run("rejects unknown action", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&ExecCmd{Args: []string{"nope", "acme"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err == nil {
			t.Error("ExecCmd with unknown action should error")
		}
	})

	t.Run("rejects too few args", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&ExecCmd{Args: []string{"noop"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err == nil {
			t.Error("ExecCmd with only action should error")
		}
	})
}

func TestEditCmd_PropagatesEditorError(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	t.Setenv("EDITOR", filepath.Join(home, "does-not-exist"))
	err := (&EditCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Error("EditCmd with missing editor should error")
	}
}

func TestApplyContexts_PlainAliasSilent(t *testing.T) {
	home := t.TempDir()
	stdout, _, err := captureStdio(func() error {
		return applyContexts(home, "plain", "pwsh", os.Stdout)
	})
	if err != nil {
		t.Fatalf("applyContexts: %v", err)
	}
	if stdout != "" {
		t.Errorf("expected no output for plain alias, got: %q", stdout)
	}
}

func TestApplyContexts_SegmentedNoFile(t *testing.T) {
	home := t.TempDir()
	// No segments.toml file; applyContexts should still succeed silently.
	_, _, err := captureStdio(func() error {
		return applyContexts(home, "src@acme", "pwsh", os.Stdout)
	})
	if err != nil {
		t.Errorf("applyContexts on missing segments: %v", err)
	}
}

// TestFastResolve_RecordsUsage guards the resolve path against silently
// regressing on frecency: every successful resolve must append to
// usage.log so `onix --stats` and tab-completion ranking reflect reality.
func TestFastResolve_RecordsUsage(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	if _, _, err := captureStdio(func() error {
		return fastResolve(home, "acme", false, os.Stdout, os.Stderr, os.Stdin)
	}); err != nil {
		t.Fatalf("fastResolve: %v", err)
	}

	usage, err := os.ReadFile(filepath.Join(home, "usage.log"))
	if err != nil {
		t.Fatalf("usage.log not created by resolve: %v", err)
	}
	if !strings.Contains(string(usage), "acme") {
		t.Errorf("usage.log does not contain the resolved alias: %q", usage)
	}
}

func TestSyncCmd(t *testing.T) {
	home := t.TempDir()
	// init sets up the directory tree and writes a base snippet.
	if err := (&InitCmd{SkipProfile: true}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}); err != nil {
		t.Fatalf("init: %v", err)
	}

	stdout, stderr, err := captureStdio(func() error {
		return (&SyncCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatalf("SyncCmd.Run: %v", err)
	}
	output := stdout + stderr
	if !strings.Contains(output, "regenerated") {
		t.Errorf("expected 'regenerated' in output: %q", output)
	}
	if !strings.Contains(output, "re-source") {
		t.Errorf("expected re-source hint in output: %q", output)
	}
}

// captureStdio runs fn with os.Stdout and os.Stderr redirected to pipes,
// returning the captured output. We restore the originals before
// returning so subsequent test logging still works.
func captureStdio(fn func() error) (stdout, stderr string, runErr error) {
	origOut, origErr := os.Stdout, os.Stderr
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	os.Stdout = outW
	os.Stderr = errW

	runErr = fn()
	outW.Close()
	errW.Close()

	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, outR)
	_, _ = io.Copy(&errBuf, errR)
	return outBuf.String(), errBuf.String(), runErr
}

func TestVersionCmd(t *testing.T) {
	home := t.TempDir()
	t.Run("plain", func(t *testing.T) {
		stdout, _, err := captureStdio(func() error {
			return (&VersionCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, "onix:") || !strings.Contains(stdout, "go:") {
			t.Errorf("version output missing labels: %q", stdout)
		}
	})

	t.Run("JSON", func(t *testing.T) {
		stdout, _, err := captureStdio(func() error {
			return (&VersionCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, JSON: true})
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(stdout, "{") {
			t.Errorf("expected JSON object, got: %q", stdout)
		}
		if !strings.Contains(stdout, `"onix"`) || !strings.Contains(stdout, `"go"`) {
			t.Errorf("JSON output missing fields: %q", stdout)
		}
	})
}

func TestFastListNames(t *testing.T) {
	home := t.TempDir()
	// Register some aliases
	_ = (&AddCmd{Alias: "a", Path: "C:/a"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	_ = (&AddCmd{Alias: "b", Path: "C:/b"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	stdout, _, err := captureStdio(func() error {
		return fastListNames(home, os.Stdout)
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), stdout)
	}
	if lines[0] != "a" || lines[1] != "b" {
		t.Errorf("expected [a, b], got %v", lines)
	}
}

func TestFastResolve_PrintsPath(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	stdout, _, err := captureStdio(func() error {
		return fastResolve(home, "acme", false, os.Stdout, os.Stderr, os.Stdin)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(strings.TrimSpace(stdout), target) {
		t.Errorf("got %q, want %q", stdout, target)
	}
}

func TestYankCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	stdout, _, err := captureStdio(func() error {
		return (&YankCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatal(err)
	}
	// Yank prints the path to stdout.
	if strings.TrimSpace(stdout) != target {
		t.Errorf("got %q, want %q", stdout, target)
	}
}

func TestExploreCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	// Fake execCommand
	origExec := execCommand
	defer func() { execCommand = origExec }()

	var lastCmd []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		lastCmd = append([]string{name}, args...)
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", "exit 0")
		}
		return exec.Command("true")
	}

	err := (&ExploreCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if runtime.GOOS == "windows" {
		if lastCmd[0] != "explorer.exe" {
			t.Errorf("got %q, want explorer.exe", lastCmd[0])
		}
	}
}

func TestRunCmd_Errors(t *testing.T) {
	home := t.TempDir()
	e := &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}

	t.Run("too few args", func(t *testing.T) {
		err := (&RunCmd{Args: []string{"acme"}}).Run(context.Background(), e)
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("expected usage error, got %v", err)
		}
	})

	t.Run("empty argv after --", func(t *testing.T) {
		target := t.TempDir()
		_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), e)
		err := (&RunCmd{Args: []string{"acme", "--"}}).Run(context.Background(), e)
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("expected usage error, got %v", err)
		}
	})
}

func TestEditCmd_NoEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	// Also clear common fallbacks
	t.Setenv("PATH", t.TempDir())

	home := t.TempDir()
	err := (&EditCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil || !strings.Contains(err.Error(), "no $EDITOR") {
		t.Errorf("expected no editor error, got %v", err)
	}
}
