package main

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// fakeLookPath swaps lookPath so tests can declare which tools are "found"
// without depending on the host's PATH.
func fakeLookPath(t *testing.T, found map[string]bool) {
	t.Helper()
	prev := lookPath
	t.Cleanup(func() { lookPath = prev })
	lookPath = func(name string) (string, error) {
		if found[name] {
			return "C:/fake/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestGrepCmd_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	err := (&GrepCmd{Args: []string{"acme", "query"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error when rg/fzf missing, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestFindCmd_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	err := (&FindCmd{Args: []string{"acme", "query"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error when fd/fzf missing, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestGrepCmd_TooFewArgs(t *testing.T) {
	err := (&GrepCmd{Args: nil}).Run(context.Background(), &env{Home: t.TempDir(), Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestFindCmd_TooFewArgs(t *testing.T) {
	err := (&FindCmd{Args: nil}).Run(context.Background(), &env{Home: t.TempDir(), Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

// TestGrepCmd_FzfNotFound exercises the second-LookPath branch (rg present,
// fzf missing). Without the lookPath indirection the test would have to
// trust the host's PATH to pin only rg.
func TestGrepCmd_FzfNotFound(t *testing.T) {
	fakeLookPath(t, map[string]bool{"rg": true})
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	err := (&GrepCmd{Args: []string{"acme", "query"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil || !strings.Contains(err.Error(), "fzf") {
		t.Errorf("expected fzf-not-found error, got %v", err)
	}
}

// TestGrepCmd_UnknownAlias surfaces the resolve error before any tool check.
func TestGrepCmd_UnknownAlias(t *testing.T) {
	home := t.TempDir()
	err := (&GrepCmd{Args: []string{"nope", "q"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestFindCmd_UnknownAlias(t *testing.T) {
	home := t.TempDir()
	err := (&FindCmd{Args: []string{"nope", "q"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestOpenSelectionsInEditor(t *testing.T) {
	// Set EDITOR to a command that does nothing
	t.Setenv("EDITOR", "true")

	if err := openSelectionsInEditor(context.Background(), ".", []string{"file.go:10:text"}, true); err != nil {
		t.Errorf("openSelectionsInEditor (grep) failed: %v", err)
	}
	if err := openSelectionsInEditor(context.Background(), ".", []string{`C:\path\file.go`, "other.go"}, false); err != nil {
		t.Errorf("openSelectionsInEditor (find) failed: %v", err)
	}
}

func TestOpenSelectionsInEditor_NoEditor(t *testing.T) {
	// Clear EDITOR and VISUAL so resolveEditor falls through to PATH probing.
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	// Declare every fallback editor as absent.
	fakeLookPath(t, map[string]bool{})

	err := openSelectionsInEditor(context.Background(), ".", []string{"file.go"}, false)
	if err == nil {
		t.Fatal("expected error when no editor is available")
	}
	if !strings.Contains(err.Error(), "no editor found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestFindPreviewCommand confirms the OS branches: Windows points fzf at
// the onix-preview.cmd shim, POSIX uses an inline bat-or-ls fallback.
func TestFindPreviewCommand(t *testing.T) {
	got := findPreviewCommand("/home/onix")
	if runtime.GOOS == "windows" {
		if !strings.Contains(got, "onix-preview.cmd") || !strings.Contains(got, "{}") {
			t.Errorf("windows preview should invoke the shim: %q", got)
		}
	} else {
		if !strings.Contains(got, "bat ") || !strings.Contains(got, "ls -la") {
			t.Errorf("posix preview should fall back from bat to ls: %q", got)
		}
	}
}
