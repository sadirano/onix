package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextListCmd(t *testing.T) {
	home := t.TempDir()
	// Create a segments.toml with contexts
	segmentsPath := filepath.Join(home, "segments.toml")
	content := `
[[contexts]]
segment = "src"
env     = { GOFLAGS = "-tags=integration" }
exec    = ["make", "dev"]
`
	if err := os.WriteFile(segmentsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureStdio(func() error {
		return (&ContextListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout, "SEGMENT") || !strings.Contains(stdout, "src") {
		t.Errorf("output missing context data: %q", stdout)
	}
	if !strings.Contains(stdout, "GOFLAGS") || !strings.Contains(stdout, "make dev") {
		t.Errorf("output missing env/exec details: %q", stdout)
	}
}

func TestContextApplyCmd(t *testing.T) {
	home := t.TempDir()
	segmentsPath := filepath.Join(home, "segments.toml")
	content := `
[[contexts]]
segment = "src"
env     = { MODE = "dev" }
exec    = ["init-script"]
`
	if err := os.WriteFile(segmentsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("pwsh", func(t *testing.T) {
		var buf bytes.Buffer
		if err := applyContexts(home, "src@acme", "pwsh", &buf); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.Contains(got, "$env:MODE = 'dev'") || !strings.Contains(got, "& 'init-script'") {
			t.Errorf("pwsh output incorrect: %q", got)
		}
	})

	t.Run("bash", func(t *testing.T) {
		var buf bytes.Buffer
		if err := applyContexts(home, "src@acme", "bash", &buf); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.Contains(got, "export MODE='dev'") || !strings.Contains(got, "'init-script'") {
			t.Errorf("bash output incorrect: %q", got)
		}
	})

	t.Run("plain alias", func(t *testing.T) {
		var buf bytes.Buffer
		if err := applyContexts(home, "acme", "pwsh", &buf); err != nil {
			t.Fatal(err)
		}
		if buf.Len() > 0 {
			t.Errorf("expected no output for plain alias, got %q", buf.String())
		}
	})
}
