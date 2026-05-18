package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShowCmd_Run(t *testing.T) {
	tempHome := t.TempDir()
	e := &env{Home: tempHome, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	ctx := context.Background()

	// Fake execCommandContext to record calls and return a dummy cmd
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	var lastCmd []string
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		lastCmd = append([]string{name}, args...)
		// Return a command that does nothing (on Windows, cmd /c exit 0; on Unix, true)
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", "exit 0")
		}
		return exec.Command("true")
	}

	t.Run("list mode", func(t *testing.T) {
		cmd := &ShowCmd{Alias: "", Args: nil}
		if err := cmd.Run(ctx, e); err != nil {
			t.Fatalf("Run: %v", err)
		}
		// On Windows, the faked call would be powershell -NoProfile ... Get-ChildItem
		// On Unix, ls -la
		if runtime.GOOS == "windows" {
			if lastCmd[0] != "powershell" {
				t.Errorf("got %q, want powershell", lastCmd[0])
			}
		} else {
			if lastCmd[0] != "ls" {
				t.Errorf("got %q, want ls", lastCmd[0])
			}
		}
	})

	t.Run("files mode", func(t *testing.T) {
		cmd := &ShowCmd{Alias: "", Args: []string{"README.md"}}
		if err := cmd.Run(ctx, e); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if runtime.GOOS == "windows" {
			if !strings.Contains(strings.Join(lastCmd, " "), "Get-Content") {
				t.Error("expected Get-Content in args")
			}
		} else {
			if lastCmd[0] != "cat" {
				t.Errorf("got %q, want cat", lastCmd[0])
			}
		}
	})

	t.Run("unknown alias", func(t *testing.T) {
		cmd := &ShowCmd{Alias: "bogus"}
		err := cmd.Run(ctx, e)
		if err == nil {
			t.Error("expected resolver error, got nil")
		}
	})
}

func TestPassthroughExit(t *testing.T) {
	origExit := exit
	defer func() { exit = origExit }()

	var exitCode int
	exit = func(code int) {
		exitCode = code
	}

	t.Run("nil error", func(t *testing.T) {
		if err := passthroughExit(nil); err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})

	t.Run("generic error", func(t *testing.T) {
		err := errors.New("boom")
		got := passthroughExit(err)
		if got == nil || !strings.Contains(got.Error(), "show: boom") {
			t.Errorf("got %v, want 'show: boom'", got)
		}
	})

	t.Run("exit error", func(t *testing.T) {
		// We need a real *exec.ExitError. easiest way is to run a failing command.
		cmd := exec.Command("go", "invalid-command")
		err := cmd.Run()
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Skip("could not generate *exec.ExitError")
		}

		_ = passthroughExit(exitErr)
		if exitCode != exitErr.ExitCode() {
			t.Errorf("got exit code %d, want %d", exitCode, exitErr.ExitCode())
		}
	})
}

func TestPsQuoteArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			"flags unchanged",
			[]string{"-Head", "--Filter"},
			[]string{"-Head", "--Filter"},
		},
		{
			"bare identifiers unchanged",
			[]string{"README.md", "main.go"},
			[]string{"README.md", "main.go"},
		},
		{
			"whitespace triggers quoting",
			[]string{"hello world", "program files"},
			[]string{"'hello world'", "'program files'"},
		},
		{
			"embedded single quotes doubled",
			[]string{"it's", "O'Reilly"},
			[]string{"'it''s'", "'O''Reilly'"},
		},
		{
			"metachars trigger quoting",
			[]string{"tab\t", "double\"quote", "single'quote", "backtick`", "dollar$", "semicolon;", "comma,"},
			[]string{"'tab\t'", "'double\"quote'", "'single''quote'", "'backtick`'", "'dollar$'", "'semicolon;'", "'comma,'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := psQuoteArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d args, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("arg %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestHasShortFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		ch   string
		want bool
	}{
		{"matches -l", []string{"-l"}, "l", true},
		{"matches clustered -la", []string{"-la"}, "l", true},
		{"matches clustered -lah", []string{"-lah"}, "l", true},
		{"does not match --long", []string{"--long"}, "l", false},
		{"does not match positional", []string{"long"}, "l", false},
		{"does not match other short", []string{"-a"}, "l", false},
		{"does not match single-dash long flag -Filter", []string{"-Filter"}, "l", false},
		{"does not match single-dash long flag -Recurse", []string{"-Recurse"}, "r", false},
		{"matches solo uppercase short -L", []string{"-L"}, "L", true},
		{"empty args", []string{}, "l", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasShortFlag(tt.args, tt.ch); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPositional(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"true with filename", []string{"README.md"}, true},
		{"true with flag and filename", []string{"-Head", "10", "README.md"}, true},
		{"false with only flags", []string{"-la", "-h"}, false},
		{"false with empty", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPositional(tt.args); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShowCmd_TargetDir(t *testing.T) {
	tempHome := t.TempDir()
	e := &env{Home: tempHome, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}

	// 1. Empty alias returns Home
	cmd := &ShowCmd{Alias: ""}
	got, err := cmd.targetDir(e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tempHome {
		t.Errorf("got %q, want %q", got, tempHome)
	}

	// 2. Known alias returns resolved path
	// We need to create an alias in the store.
	aliasDir := filepath.Join(tempHome, "demo")
	if err := os.MkdirAll(aliasDir, 0o755); err != nil {
		t.Fatalf("failed to create alias dir: %v", err)
	}

	// Create aliases.toml
	aliasesContent := `[demo]
path = "` + filepath.ToSlash(aliasDir) + `"`
	if err := os.WriteFile(filepath.Join(tempHome, "aliases.toml"), []byte(aliasesContent), 0o644); err != nil {
		t.Fatalf("failed to write aliases.toml: %v", err)
	}

	cmd = &ShowCmd{Alias: "demo"}
	got, err = cmd.targetDir(e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != aliasDir {
		t.Errorf("got %q, want %q", got, aliasDir)
	}

	// 3. Unknown alias returns error
	cmd = &ShowCmd{Alias: "nonexistent"}
	_, err = cmd.targetDir(e)
	if err == nil {
		t.Error("expected error for unknown alias, got nil")
	}
}

func TestBuildShowCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("list mode", func(t *testing.T) {
		cmd, err := buildShowCommand(ctx, "list", []string{"-Filter", "*.go"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if runtime.GOOS == "windows" {
			// Windows: powershell -NoProfile -NonInteractive -Command Get-ChildItem -Filter *.go
			if cmd.Path != "powershell" && !strings.HasSuffix(cmd.Path, "powershell.exe") {
				t.Errorf("got path %q, want powershell", cmd.Path)
			}
			found := false
			for _, arg := range cmd.Args {
				if strings.Contains(arg, "Get-ChildItem") && strings.Contains(arg, "-Filter") && strings.Contains(arg, "*.go") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("could not find expected Get-ChildItem script in args: %v", cmd.Args)
			}
		} else {
			// Unix: ls -la -Filter *.go. Path is LookPath-resolved
			// (/usr/bin/ls etc.), so compare basename rather than the
			// literal string.
			if base := filepath.Base(cmd.Path); base != "ls" {
				t.Errorf("got path %q, want basename ls", cmd.Path)
			}
			hasLA := false
			for _, a := range cmd.Args {
				if a == "-la" {
					hasLA = true
					break
				}
			}
			if !hasLA {
				t.Errorf("-la not found in args: %v", cmd.Args)
			}
		}
	})

	t.Run("files mode", func(t *testing.T) {
		cmd, err := buildShowCommand(ctx, "files", []string{"README.md"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if runtime.GOOS == "windows" {
			found := false
			for _, arg := range cmd.Args {
				if strings.Contains(arg, "Get-Content") && strings.Contains(arg, "README.md") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("could not find expected Get-Content script in args: %v", cmd.Args)
			}
		} else {
			if base := filepath.Base(cmd.Path); base != "cat" {
				t.Errorf("got path %q, want basename cat", cmd.Path)
			}
			found := false
			for _, a := range cmd.Args {
				if a == "README.md" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("README.md not found in args: %v", cmd.Args)
			}
		}
	})
}
