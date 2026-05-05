package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

func TestResolveContext(t *testing.T) {
	t.Run("no context section returns empty string", func(t *testing.T) {
		cfg := &config.Config{}
		ctx, err := resolveContext(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "" {
			t.Errorf("got %q, want empty string", ctx)
		}
	})

	t.Run("env source with var set", func(t *testing.T) {
		t.Setenv("TEST_CTX_VAR", "12345")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "TEST_CTX_VAR"},
		}
		ctx, err := resolveContext(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "12345" {
			t.Errorf("got %q, want %q", ctx, "12345")
		}
	})

	t.Run("env source with var unset returns error", func(t *testing.T) {
		t.Setenv("TEST_CTX_MISSING", "")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "TEST_CTX_MISSING"},
		}
		_, err := resolveContext(cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("env source with empty var name returns error", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: ""},
		}
		_, err := resolveContext(cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("file source with existing file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "ctx")
		if err := os.WriteFile(p, []byte("branch-abc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "file", File: p},
		}
		ctx, err := resolveContext(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "branch-abc" {
			t.Errorf("got %q, want %q", ctx, "branch-abc")
		}
	})

	t.Run("file source with missing file returns error", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "file", File: "/nonexistent/path/ctx"},
		}
		_, err := resolveContext(cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("file source with empty file returns error", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "ctx")
		if err := os.WriteFile(p, []byte("   \n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "file", File: p},
		}
		_, err := resolveContext(cfg)
		if err == nil {
			t.Fatal("expected error for empty file, got nil")
		}
	})

	t.Run("cmd source with echo command", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "cmd", Cmd: "echo hello"},
		}
		ctx, err := resolveContext(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "hello" {
			t.Errorf("got %q, want %q", ctx, "hello")
		}
	})

	t.Run("cmd source with empty cmd string returns error", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "cmd", Cmd: ""},
		}
		_, err := resolveContext(cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input string
		want  string
	}{
		{"~/.onix/ctx", home + "/.onix/ctx"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~", home},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := expandTilde(tt.input); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
