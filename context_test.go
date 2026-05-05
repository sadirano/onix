package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

func TestAliasContextConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)

	t.Run("write env config then load", func(t *testing.T) {
		cc := config.ContextConfig{Source: "env", Var: "MY_CTX"}
		if err := writeAliasContextConfig("sms", cc); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, ok := loadAliasContextConfig("sms")
		if !ok {
			t.Fatal("expected config to be present")
		}
		if got.Source != "env" || got.Var != "MY_CTX" {
			t.Errorf("got source=%q var=%q, want env/MY_CTX", got.Source, got.Var)
		}
	})

	t.Run("write cmd config then load", func(t *testing.T) {
		cc := config.ContextConfig{Source: "cmd", Cmd: "git branch --show-current"}
		if err := writeAliasContextConfig("proj", cc); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, ok := loadAliasContextConfig("proj")
		if !ok {
			t.Fatal("expected config to be present")
		}
		if got.Source != "cmd" || got.Cmd != "git branch --show-current" {
			t.Errorf("got source=%q cmd=%q", got.Source, got.Cmd)
		}
	})

	t.Run("write file config then load", func(t *testing.T) {
		cc := config.ContextConfig{Source: "file", File: "~/.onix/ctx"}
		if err := writeAliasContextConfig("work", cc); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, ok := loadAliasContextConfig("work")
		if !ok {
			t.Fatal("expected config to be present")
		}
		if got.Source != "file" || got.File != "~/.onix/ctx" {
			t.Errorf("got source=%q file=%q", got.Source, got.File)
		}
	})

	t.Run("load missing alias returns false", func(t *testing.T) {
		_, ok := loadAliasContextConfig("does-not-exist-xyz")
		if ok {
			t.Error("expected false for unconfigured alias")
		}
	})

	t.Run("clear removes config", func(t *testing.T) {
		cc := config.ContextConfig{Source: "env", Var: "V"}
		if err := writeAliasContextConfig("tmp-alias", cc); err != nil {
			t.Fatal(err)
		}
		if err := clearAliasContext("tmp-alias"); err != nil {
			t.Fatalf("clear: %v", err)
		}
		_, ok := loadAliasContextConfig("tmp-alias")
		if ok {
			t.Error("expected false after clear")
		}
	})

	t.Run("clear on missing alias is no-op", func(t *testing.T) {
		if err := clearAliasContext("never-set-xyz"); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("write overwrites previous config", func(t *testing.T) {
		if err := writeAliasContextConfig("x", config.ContextConfig{Source: "env", Var: "V1"}); err != nil {
			t.Fatal(err)
		}
		if err := writeAliasContextConfig("x", config.ContextConfig{Source: "cmd", Cmd: "echo v2"}); err != nil {
			t.Fatal(err)
		}
		got, ok := loadAliasContextConfig("x")
		if !ok || got.Source != "cmd" || got.Cmd != "echo v2" {
			t.Errorf("got source=%q cmd=%q", got.Source, got.Cmd)
		}
	})
}

func TestResolveContext(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)

	t.Run("no config and no global returns empty string", func(t *testing.T) {
		cfg := &config.Config{}
		ctx, err := resolveContext("unset-alias", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "" {
			t.Errorf("got %q, want empty", ctx)
		}
	})

	t.Run("per-alias config takes priority over global", func(t *testing.T) {
		t.Setenv("GLOBAL_VAR", "global")
		t.Setenv("ALIAS_VAR", "alias-specific")
		if err := writeAliasContextConfig("priority-alias", config.ContextConfig{Source: "env", Var: "ALIAS_VAR"}); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "GLOBAL_VAR"},
		}
		ctx, err := resolveContext("priority-alias", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "alias-specific" {
			t.Errorf("got %q, want alias-specific", ctx)
		}
	})

	t.Run("falls back to global when no alias config", func(t *testing.T) {
		t.Setenv("FALLBACK_VAR", "fallback")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "FALLBACK_VAR"},
		}
		ctx, err := resolveContext("no-alias-config", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "fallback" {
			t.Errorf("got %q, want fallback", ctx)
		}
	})

	t.Run("env source var unset returns error", func(t *testing.T) {
		t.Setenv("UNSET_VAR", "")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "UNSET_VAR"},
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

	t.Run("cmd source with echo", func(t *testing.T) {
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
