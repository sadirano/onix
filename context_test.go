package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

func TestApplyContextTemplate(t *testing.T) {
	tests := []struct {
		template string
		varName  string
		value    string
		want     string
	}{
		{"", "", "12345", "12345"},
		{"{value}", "", "12345", "12345"},
		{"/{value}", "", "12345", "12345"},
		{"task/{value}", "", "12345", "task/12345"},
		{"/task/{value}", "", "12345", "task/12345"},
		{"client/{value}/docs", "", "abc", "client/abc/docs"},
		// named-var placeholder: {varName} substituted like {value}
		{"/tes/{test}", "test", "123", "tes/123"},
		{"tes/{test}/sub", "test", "abc", "tes/abc/sub"},
		// both placeholders present
		{"{test}/{value}", "test", "x", "x/x"},
	}
	for _, tt := range tests {
		t.Run(tt.template+"|"+tt.value, func(t *testing.T) {
			if got := applyContextTemplate(tt.template, tt.varName, tt.value); got != tt.want {
				t.Errorf("applyContextTemplate(%q, %q, %q) = %q, want %q", tt.template, tt.varName, tt.value, got, tt.want)
			}
		})
	}
}

func TestAliasContextConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)

	t.Run("write alias config then load", func(t *testing.T) {
		cc := config.ContextConfig{Source: "alias", Path: "a/sub/dir"}
		if err := writeAliasContextConfig("fix-seg", cc); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, ok := loadAliasContextConfig("fix-seg")
		if !ok {
			t.Fatal("expected config to be present")
		}
		if got.Source != "alias" || got.Path != "a/sub/dir" {
			t.Errorf("got source=%q path=%q, want alias/a/sub/dir", got.Source, got.Path)
		}
	})

	t.Run("alias source resolves to literal path", func(t *testing.T) {
		cc := config.ContextConfig{Source: "alias", Path: "some/fixed/path"}
		v, err := resolveContextConfig(cc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "some/fixed/path" {
			t.Errorf("got %q, want some/fixed/path", v)
		}
	})

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

	t.Run("template is persisted", func(t *testing.T) {
		cc := config.ContextConfig{Source: "env", Var: "TASK_ID", Template: "task/{value}"}
		if err := writeAliasContextConfig("task", cc); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, ok := loadAliasContextConfig("task")
		if !ok {
			t.Fatal("expected config to be present")
		}
		if got.Template != "task/{value}" {
			t.Errorf("got template=%q, want task/{value}", got.Template)
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

	t.Run("load missing segment returns false", func(t *testing.T) {
		_, ok := loadAliasContextConfig("does-not-exist-xyz")
		if ok {
			t.Error("expected false for unconfigured segment")
		}
	})

	t.Run("clear removes config", func(t *testing.T) {
		cc := config.ContextConfig{Source: "env", Var: "V"}
		if err := writeAliasContextConfig("tmp-seg", cc); err != nil {
			t.Fatal(err)
		}
		if err := clearAliasContext("tmp-seg"); err != nil {
			t.Fatalf("clear: %v", err)
		}
		_, ok := loadAliasContextConfig("tmp-seg")
		if ok {
			t.Error("expected false after clear")
		}
	})

	t.Run("clear on missing is no-op", func(t *testing.T) {
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
		ctx, err := resolveContext("unset-seg", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "" {
			t.Errorf("got %q, want empty", ctx)
		}
	})

	t.Run("per-segment config takes priority over global", func(t *testing.T) {
		t.Setenv("GLOBAL_VAR", "global")
		t.Setenv("SEG_VAR", "seg-specific")
		if err := writeAliasContextConfig("my-seg", config.ContextConfig{Source: "env", Var: "SEG_VAR"}); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "GLOBAL_VAR"},
		}
		ctx, err := resolveContext("my-seg", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "seg-specific" {
			t.Errorf("got %q, want seg-specific", ctx)
		}
	})

	t.Run("falls back to global when no segment config", func(t *testing.T) {
		t.Setenv("FALLBACK_VAR", "fallback")
		cfg := &config.Config{
			Context: config.ContextConfig{Source: "env", Var: "FALLBACK_VAR"},
		}
		ctx, err := resolveContext("no-config-seg", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx != "fallback" {
			t.Errorf("got %q, want fallback", ctx)
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
		ctx, err := resolveContext("no-seg", cfg)
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
		ctx, err := resolveContext("no-seg", cfg)
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
