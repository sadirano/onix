package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

// redirectContextDir points the per-alias context store at dir for the test.
func redirectContextDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("ONIX_HOME", dir) // config.Dir() reads ONIX_HOME when set — not currently wired;
	// we override aliasContextPath indirectly by pointing ONIX_HOME isn't wired yet,
	// so we test getAliasContext / setAliasContext via the real home dir in a tempdir.
	// Tests that need isolation call the low-level functions directly.
	_ = dir
}

func TestAliasContextStore(t *testing.T) {
	// Run storage tests in a temp dir by temporarily swapping USERPROFILE so
	// config.Dir() resolves there, keeping the real ~/.onix untouched.
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)

	t.Run("set then get returns value", func(t *testing.T) {
		if err := setAliasContext("sms", "12345"); err != nil {
			t.Fatalf("set: %v", err)
		}
		v, ok := getAliasContext("sms")
		if !ok || v != "12345" {
			t.Errorf("get: got (%q, %v), want (\"12345\", true)", v, ok)
		}
	})

	t.Run("get with no file returns false", func(t *testing.T) {
		_, ok := getAliasContext("nonexistent-alias-xyz")
		if ok {
			t.Error("expected false for unset alias")
		}
	})

	t.Run("clear removes file", func(t *testing.T) {
		if err := setAliasContext("proj", "branch-main"); err != nil {
			t.Fatalf("set: %v", err)
		}
		if err := clearAliasContext("proj"); err != nil {
			t.Fatalf("clear: %v", err)
		}
		_, ok := getAliasContext("proj")
		if ok {
			t.Error("expected false after clear")
		}
	})

	t.Run("clear on missing alias is a no-op", func(t *testing.T) {
		if err := clearAliasContext("does-not-exist-xyz"); err != nil {
			t.Errorf("clear of missing alias should not error: %v", err)
		}
	})

	t.Run("set overwrites previous value", func(t *testing.T) {
		if err := setAliasContext("work", "v1"); err != nil {
			t.Fatal(err)
		}
		if err := setAliasContext("work", "v2"); err != nil {
			t.Fatal(err)
		}
		v, ok := getAliasContext("work")
		if !ok || v != "v2" {
			t.Errorf("got (%q, %v), want (\"v2\", true)", v, ok)
		}
	})
}

func TestResolveContext(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)

	t.Run("no context section and no pinned value returns empty string", func(t *testing.T) {
		cfg := &config.Config{}
		ctx, err := resolveContext("unset-alias", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "" {
			t.Errorf("got %q, want empty string", ctx)
		}
	})

	t.Run("pinned alias context takes priority over global config", func(t *testing.T) {
		t.Setenv("GLOBAL_CTX_VAR", "global-value")
		if err := setAliasContext("pinned-alias", "pinned-value"); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "GLOBAL_CTX_VAR"},
		}
		ctx, err := resolveContext("pinned-alias", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "pinned-value" {
			t.Errorf("got %q, want pinned-value", ctx)
		}
	})

	t.Run("falls back to global config when no alias-specific context", func(t *testing.T) {
		t.Setenv("TEST_CTX_VAR", "12345")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "TEST_CTX_VAR"},
		}
		ctx, err := resolveContext("alias-without-pin", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "12345" {
			t.Errorf("got %q, want 12345", ctx)
		}
	})

	t.Run("env source with var unset returns error", func(t *testing.T) {
		t.Setenv("TEST_CTX_MISSING", "")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "TEST_CTX_MISSING"},
		}
		_, err := resolveContext("no-pin", cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("env source with empty var name returns error", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: ""},
		}
		_, err := resolveContext("no-pin", cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("file source with existing file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "ctx")
		if err := os.WriteFile(p, []byte("branch-abc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "file", File: p},
		}
		ctx, err := resolveContext("no-pin", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "branch-abc" {
			t.Errorf("got %q, want branch-abc", ctx)
		}
	})

	t.Run("file source with missing file returns error", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "file", File: "/nonexistent/path/ctx"},
		}
		_, err := resolveContext("no-pin", cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("cmd source with echo command", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "cmd", Cmd: "echo hello"},
		}
		ctx, err := resolveContext("no-pin", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "hello" {
			t.Errorf("got %q, want hello", ctx)
		}
	})

	t.Run("cmd source with empty cmd string returns error", func(t *testing.T) {
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "cmd", Cmd: ""},
		}
		_, err := resolveContext("no-pin", cfg)
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
