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
	err := (&GrepCmd{Args: []string{"nope", "q"}}).Run(context.Background(), &env{Home: home, NoPrompt: true, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestFindCmd_UnknownAlias(t *testing.T) {
	home := t.TempDir()
	err := (&FindCmd{Args: []string{"nope", "q"}}).Run(context.Background(), &env{Home: home, NoPrompt: true, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
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

// TestOpensWithDefaultApp locks the ff routing decision: allowlisted
// view-only types and directories open with the OS handler; source,
// configs, and — crucially — executables/scripts fall through to the
// editor so a found file is never auto-run.
func TestOpensWithDefaultApp(t *testing.T) {
	defaultApp := []string{"report.pdf", "Slides.PPTX", "photo.JPG", "bundle.zip", "clip.mp4"}
	for _, p := range defaultApp {
		if !opensWithDefaultApp(p) {
			t.Errorf("expected %q to open with default app", p)
		}
	}

	editor := []string{"main.go", "notes.md", "config.toml", "data.json", "noext"}
	for _, p := range editor {
		if opensWithDefaultApp(p) {
			t.Errorf("expected %q to route to the editor", p)
		}
	}

	// Safety guarantee: executables/scripts must never default-open.
	executable := []string{"run.cmd", "tool.exe", "deploy.ps1", "setup.bat", "installer.msi", "x.scr"}
	for _, p := range executable {
		if opensWithDefaultApp(p) {
			t.Errorf("SECURITY: %q must route to the editor, not auto-launch", p)
		}
	}

	// A directory opens as a folder regardless of name.
	if !opensWithDefaultApp(t.TempDir()) {
		t.Error("expected a directory to open with the OS file manager")
	}
}

func TestRelaxNonASCII(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"hello", "hello"},
		{"café", "caf."},
		{"áéíóú", "....."},
		{"id > 10", "id > 10"},
		{"\\d+", "\\d+"},
		{"José da Silva", "Jos. da Silva"},
	}
	for _, tc := range cases {
		if got := relaxNonASCII(tc.in); got != tc.want {
			t.Errorf("relaxNonASCII(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
