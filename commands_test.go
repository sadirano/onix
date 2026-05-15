package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		return (&AddCmd{Alias: "acme", Path: target}).Run(&env{Home: home})
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
		return (&AddCmd{Alias: "newdir", Path: target}).Run(&env{Home: home})
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
			return (&AddCmd{Alias: name, Path: home}).Run(&env{Home: home})
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
	pathA, _ := filepath.Abs("a")
	pathB, _ := filepath.Abs("b")
	(&AddCmd{Alias: "a", Path: pathA}).Run(&env{Home: home})
	(&AddCmd{Alias: "b", Path: pathB}).Run(&env{Home: home})

	t.Run("table output", func(t *testing.T) {
		stdout, _, err := captureStdio(func() error {
			return (&ListCmd{}).Run(&env{Home: home})
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
			return (&ListCmd{}).Run(&env{Home: home, JSON: true})
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

	(&AddCmd{Alias: "acme", Path: "C:/acme"}).Run(&env{Home: home})

	t.Run("remove existing", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RemoveCmd{Alias: "acme"}).Run(&env{Home: home})
		})
		if err != nil {
			t.Fatalf("RemoveCmd.Run: %v", err)
		}
		// Confirm it's gone from List.
		stdout, _, _ := captureStdio(func() error {
			return (&ListCmd{}).Run(&env{Home: home})
		})
		if strings.Contains(stdout, "acme") {
			t.Error("alias still present after removal")
		}
	})

	t.Run("remove missing", func(t *testing.T) {
		_, _, err := captureStdio(func() error {
			return (&RemoveCmd{Alias: "nope"}).Run(&env{Home: home})
		})
		if err == nil {
			t.Error("RemoveCmd on missing alias should have errored")
		}
	})
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
			return (&VersionCmd{}).Run(&env{Home: home})
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
			return (&VersionCmd{}).Run(&env{Home: home, JSON: true})
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

func TestListNamesCmd(t *testing.T) {
	home := t.TempDir()
	// Register some aliases
	(&AddCmd{Alias: "a", Path: "C:/a"}).Run(&env{Home: home})
	(&AddCmd{Alias: "b", Path: "C:/b"}).Run(&env{Home: home})

	stdout, _, err := captureStdio(func() error {
		return (&ListNamesCmd{}).Run(&env{Home: home})
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

func TestResolveCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	(&AddCmd{Alias: "acme", Path: target}).Run(&env{Home: home})

	stdout, _, err := captureStdio(func() error {
		return (&ResolveCmd{Alias: "acme"}).Run(&env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != target {
		t.Errorf("got %q, want %q", stdout, target)
	}
}

func TestYankCmd(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	(&AddCmd{Alias: "acme", Path: target}).Run(&env{Home: home})

	stdout, _, err := captureStdio(func() error {
		return (&YankCmd{Alias: "acme"}).Run(&env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	// Yank prints the path to stdout.
	if strings.TrimSpace(stdout) != target {
		t.Errorf("got %q, want %q", stdout, target)
	}
}

func TestFastResolve(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	(&AddCmd{Alias: "acme", Path: target}).Run(&env{Home: home})

	stdout, _, err := captureStdio(func() error {
		return fastResolve(home, "acme")
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != target {
		t.Errorf("got %q, want %q", stdout, target)
	}
}

func TestFastListNames(t *testing.T) {
	home := t.TempDir()
	(&AddCmd{Alias: "a", Path: "C:/a"}).Run(&env{Home: home})
	(&AddCmd{Alias: "b", Path: "C:/b"}).Run(&env{Home: home})

	stdout, _, err := captureStdio(func() error {
		return fastListNames(home)
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), stdout)
	}
}
