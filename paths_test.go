package main

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveHome(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		got, err := resolveHome("/tmp/foo")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(filepath.ToSlash(got), "/tmp/foo") {
			t.Errorf("got %q, want suffix /tmp/foo", got)
		}
	})

	t.Run("default", func(t *testing.T) {
		got, err := resolveHome("")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(filepath.ToSlash(got), "/.onix") {
			t.Errorf("got %q, want suffix /.onix", got)
		}
	})
}

func TestStartsWithDash(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"-h", true},
		{"--help", true},
		{"alias", false},
		{"", false},
		{"-", true},
	}
	for _, tc := range tests {
		got := startsWithDash(tc.in)
		if got != tc.want {
			t.Errorf("startsWithDash(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPwshBin(t *testing.T) {
	bin := pwshBin()
	if bin == "" {
		t.Error("pwshBin returned empty string")
	}
}

// TestPwshBin_FallbackBranches exercises the runtime-conditional fallbacks
// when `pwsh` isn't on PATH.
func TestPwshBin_FallbackBranches(t *testing.T) {
	prev := lookPath
	t.Cleanup(func() { lookPath = prev })
	lookPath = func(name string) (string, error) {
		if name == "pwsh" {
			return "/fake/pwsh", nil
		}
		return "", errors.New("not found")
	}
	if got := pwshBin(); got != "pwsh" {
		t.Errorf("with pwsh on PATH, got %q, want pwsh", got)
	}

	// Now pretend pwsh is missing.
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	got := pwshBin()
	if runtime.GOOS == "windows" {
		if got != "powershell.exe" {
			t.Errorf("on Windows without pwsh, got %q, want powershell.exe", got)
		}
	} else {
		if got != "pwsh" {
			t.Errorf("on non-Windows fallback, got %q, want pwsh", got)
		}
	}
}

// TestResolveHome_TildeOverride confirms ~ expansion runs on the override.
func TestResolveHome_TildeOverride(t *testing.T) {
	got, err := resolveHome("~/foo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(got, "~") {
		t.Errorf("tilde not expanded: %q", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "/foo") {
		t.Errorf("got %q, want suffix /foo", got)
	}
}
