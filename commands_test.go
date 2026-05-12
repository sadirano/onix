package main

import (
	"bytes"
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
