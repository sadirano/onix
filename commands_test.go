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
	"github.com/sadirano/onix/internal/usage"
)

// testTarget returns an alias target inside a test-owned temp dir. AddCmd
// MkdirAlls its target, so fixture paths must never point at real locations
// (a bare "C:/acme" or relative "a") — that litters the developer's drive
// root and the repo working tree with stray directories.
func testTarget(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

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
		return (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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
		return (&AddCmd{Alias: "newdir", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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
			return (&AddCmd{Alias: name, Path: home}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err == nil {
			t.Errorf("AddCmd with name %q should have errored", name)
		}
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
	pathA := filepath.Join(dir, "a")
	pathB := filepath.Join(dir, "b")
	_ = (&AddCmd{Alias: "a", Path: pathA}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
	_ = (&AddCmd{Alias: "b", Path: pathB}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	t.Run("table output", func(t *testing.T) {
		stdout, _, err := captureStdio(func() error {
			return (&ListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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

	_ = (&AddCmd{Alias: "acme", Path: testTarget(t, "acme")}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	t.Run("remove existing", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RemoveCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err != nil {
			t.Fatalf("RemoveCmd.Run: %v", err)
		}
		// Confirm it's gone from List.
		stdout, _, _ := captureStdio(func() error {
			return (&ListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if strings.Contains(stdout, "acme") {
			t.Error("alias still present after removal")
		}
	})

	t.Run("remove missing", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RemoveCmd{Alias: "nope"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	bin, args := noopExec()

	t.Run("happy path", func(t *testing.T) {
		argv := append([]string{"acme", bin}, args...)
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: argv}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err != nil {
			t.Errorf("RunCmd.Run: %v", err)
		}
	})

	t.Run("strips leading double-dash", func(t *testing.T) {
		argv := append([]string{"acme", "--", bin}, args...)
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: argv}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err != nil {
			t.Errorf("RunCmd.Run with -- separator: %v", err)
		}
	})

	// Detached (-o/--outside) children are not waited on, so their cwd must
	// not live in a t.TempDir: on slow runners (Windows CI) the child can
	// still hold the directory when the framework deletes it, failing
	// cleanup with a sharing violation. Park detached runs in the system
	// temp dir, which nothing removes.
	_ = (&AddCmd{Alias: "acmeout", Path: os.TempDir()}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	t.Run("happy path with -o flag", func(t *testing.T) {
		argv := append([]string{"acmeout", "-o", bin}, args...)
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: argv}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err != nil {
			t.Errorf("RunCmd.Run with -o: %v", err)
		}
	})

	t.Run("happy path with --outside flag", func(t *testing.T) {
		argv := append([]string{"acmeout", "--outside", bin}, args...)
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: argv}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err != nil {
			t.Errorf("RunCmd.Run with --outside: %v", err)
		}
	})

	t.Run("rejects too few args", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: []string{"acme"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err == nil {
			t.Error("RunCmd with only alias should error")
		}
	})

	t.Run("rejects empty argv after --", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RunCmd{Args: []string{"acme", "--"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
		})
		if err == nil {
			t.Error("RunCmd with bare -- should error")
		}
	})
}

func TestEditCmd_PropagatesEditorError(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	t.Setenv("EDITOR", filepath.Join(home, "does-not-exist"))
	err := (&EditCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
	if err == nil {
		t.Error("EditCmd with missing editor should error")
	}
}

func TestSyncCmd(t *testing.T) {
	// Isolate clink's profile dir: sync refreshes %LOCALAPPDATA%\clink\onix.lua
	// when present, and the test must not touch the developer's real one.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	home := t.TempDir()
	// init sets up the directory tree and writes a base snippet.
	if err := (&InitCmd{SkipProfile: true}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true}); err != nil {
		t.Fatalf("init: %v", err)
	}

	stdout, stderr, err := captureStdio(func() error {
		return (&SyncCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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
			return (&VersionCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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
	_ = (&AddCmd{Alias: "a", Path: testTarget(t, "a")}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
	_ = (&AddCmd{Alias: "b", Path: testTarget(t, "b")}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

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
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	stdout, _, err := captureStdio(func() error {
		return fastResolve(home, "acme", os.Stdout, os.Stderr, os.Stdin, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(strings.TrimSpace(stdout), target) {
		t.Errorf("got %q, want %q", stdout, target)
	}
}

// stubFzf swaps the exec seams so PruneCmd's fzf invocation runs the given
// command instead. Restored on cleanup.
func stubFzf(t *testing.T, cmd func() *exec.Cmd) {
	t.Helper()
	prevLook, prevCtx := lookPath, execCommandContext
	t.Cleanup(func() { lookPath, execCommandContext = prevLook, prevCtx })
	lookPath = func(name string) (string, error) { return "C:/fake/" + name, nil }
	execCommandContext = func(context.Context, string, ...string) *exec.Cmd { return cmd() }
}

// echoCmd returns a command that prints s — a fake fzf "selection".
func echoCmd(s string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "echo "+s)
	}
	return exec.Command("echo", s)
}

// exitCmd returns a command exiting with the given code — a fake fzf cancel.
func exitCmd(code int) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", fmt.Sprintf("exit %d", code))
	}
	return exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
}

func TestPruneCmd_RemovesSelected(t *testing.T) {
	home := t.TempDir()
	e := &env{Home: home, Stdout: io.Discard, Stderr: io.Discard, Stdin: os.Stdin}
	_ = (&AddCmd{Alias: "stale", Path: testTarget(t, "stale")}).Run(context.Background(), e)
	_ = (&AddCmd{Alias: "keeper", Path: testTarget(t, "keeper")}).Run(context.Background(), e)

	stubFzf(t, func() *exec.Cmd { return echoCmd("stale     never     0 uses  C:/whatever") })
	if err := (&PruneCmd{}).Run(context.Background(), e); err != nil {
		t.Fatalf("PruneCmd.Run: %v", err)
	}

	s, err := store.LoadStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup("stale"); ok {
		t.Error("stale still registered after prune")
	}
	if _, ok := s.Lookup("keeper"); !ok {
		t.Error("keeper was pruned but not selected")
	}
	if _, ok := usage.Load(home)["stale"]; ok {
		t.Error("usage entry for pruned alias not cleaned up")
	}
}

func TestPruneCmd_CancelRemovesNothing(t *testing.T) {
	home := t.TempDir()
	e := &env{Home: home, Stdout: io.Discard, Stderr: io.Discard, Stdin: os.Stdin}
	_ = (&AddCmd{Alias: "stale", Path: testTarget(t, "stale")}).Run(context.Background(), e)

	stubFzf(t, func() *exec.Cmd { return exitCmd(130) })
	if err := (&PruneCmd{}).Run(context.Background(), e); err != nil {
		t.Fatalf("cancelled prune must not error: %v", err)
	}

	s, _ := store.LoadStore(home)
	if _, ok := s.Lookup("stale"); !ok {
		t.Error("cancelled prune removed an alias")
	}
}

func TestPruneCmd_NoPromptPrintsRankingOnly(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := &env{Home: home, Stdout: &out, Stderr: io.Discard, Stdin: os.Stdin, NoPrompt: true}

	// "gone" gets a dead target (registered, then directory deleted) so it
	// must rank first with the [gone] marker; "alive" stays intact.
	goneTarget := testTarget(t, "gone")
	_ = (&AddCmd{Alias: "gone", Path: goneTarget}).Run(context.Background(), e)
	_ = (&AddCmd{Alias: "alive", Path: testTarget(t, "alive")}).Run(context.Background(), e)
	if err := os.RemoveAll(goneTarget); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := (&PruneCmd{}).Run(context.Background(), e); err != nil {
		t.Fatalf("PruneCmd.Run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 ranking lines, got %q", out.String())
	}
	if !strings.HasPrefix(lines[0], "gone") || !strings.Contains(lines[0], "[gone]") {
		t.Errorf("dead alias not ranked first with marker: %q", lines[0])
	}

	s, _ := store.LoadStore(home)
	if len(s.Aliases) != 2 {
		t.Errorf("--no-prompt prune deleted aliases: %v", s.Names())
	}
}

func TestFastResolve_RecordsDebouncedUsage(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	e := &env{Home: home, Stdout: io.Discard, Stderr: io.Discard, Stdin: os.Stdin, NoPrompt: true}
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), e)

	// The add itself counts as the first use; immediate resolves fall
	// inside the debounce window and must not bump the count again.
	for range 2 {
		if _, _, err := captureStdio(func() error {
			return fastResolve(home, "acme", os.Stdout, os.Stderr, os.Stdin, nil)
		}); err != nil {
			t.Fatal(err)
		}
	}

	ent, ok := usage.Load(home)["acme"]
	if !ok {
		t.Fatal("no usage entry after add+resolves")
	}
	if ent.Count != 1 {
		t.Errorf("debounce failed: count = %d, want 1", ent.Count)
	}
	if ent.Last == 0 {
		t.Error("last-used stamp missing")
	}
}

func TestYankCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

	stdout, _, err := captureStdio(func() error {
		return (&YankCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
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
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})

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

	err := (&ExploreCmd{Alias: "acme"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if runtime.GOOS == "windows" {
		if lastCmd[0] != "explorer.exe" {
			t.Errorf("got %q, want explorer.exe", lastCmd[0])
		}
	} else {
		if lastCmd[0] != "xdg-open" {
			t.Errorf("got %q, want xdg-open", lastCmd[0])
		}
	}
}

func TestRunCmd_Errors(t *testing.T) {
	home := t.TempDir()
	e := &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true}

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
	err := (&EditCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, NoPrompt: true})
	if err == nil || !strings.Contains(err.Error(), "no $EDITOR") {
		t.Errorf("expected no editor error, got %v", err)
	}
}
