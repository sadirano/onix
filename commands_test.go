package main

import (
	"reflect"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

func TestParseExtras(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSubdir string
		wantExtras []string
	}{
		{"no args", nil, "", nil},
		{"subdir short flag", []string{"-s", "cmd"}, "cmd", nil},
		{"subdir long flag", []string{"--subdir", "pkg/api"}, "pkg/api", nil},
		{"extras collected", []string{"foo.go", "bar.go"}, "", []string{"foo.go", "bar.go"}},
		{"subdir and extras", []string{"-s", "pkg", "foo.go"}, "pkg", []string{"foo.go"}},
		{"extras and subdir", []string{"foo.go", "-s", "sub"}, "sub", []string{"foo.go"}},
		{"subdir missing value is no-op", []string{"-s"}, "", nil},
		{"unknown flag treated as extra", []string{"-sg", "foo"}, "", []string{"-sg", "foo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subdir, extras := parseExtras(tt.args)
			if subdir != tt.wantSubdir {
				t.Errorf("subdir: got %q, want %q", subdir, tt.wantSubdir)
			}
			if !reflect.DeepEqual(extras, tt.wantExtras) {
				t.Errorf("extras: got %v, want %v", extras, tt.wantExtras)
			}
		})
	}
}

func TestParseSubAlias(t *testing.T) {
	tests := []struct {
		input        string
		wantSubAlias string
		wantAlias    string
	}{
		{"an@sms", "an", "sms"},
		{"sms", "", "sms"},
		{"@sms", "", "sms"},
		{"a@b@c", "a", "b@c"},
		{"", "", ""},
		{"sub@", "sub", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotSub, gotAlias := parseSubAlias(tt.input)
			if gotSub != tt.wantSubAlias || gotAlias != tt.wantAlias {
				t.Errorf("parseSubAlias(%q) = (%q, %q), want (%q, %q)",
					tt.input, gotSub, gotAlias, tt.wantSubAlias, tt.wantAlias)
			}
		})
	}
}

func TestResolveBuiltin(t *testing.T) {
	t.Run("empty cmdName returns shell", func(t *testing.T) {
		cfg := &config.Config{}
		if got := resolveBuiltin("", cfg); got != "shell" {
			t.Errorf("got %q, want shell", got)
		}
	})
	t.Run("default editor action", func(t *testing.T) {
		cfg := &config.Config{}
		if got := resolveBuiltin("editor", cfg); got != "editor" {
			t.Errorf("got %q, want editor", got)
		}
	})
	t.Run("default explore action", func(t *testing.T) {
		cfg := &config.Config{}
		if got := resolveBuiltin("explore", cfg); got != "explorer" {
			t.Errorf("got %q, want explorer", got)
		}
	})
	t.Run("custom action with overridden builtin", func(t *testing.T) {
		cfg := &config.Config{
			Actions: []config.Action{
				{Name: "myshell", Builtin: "shell"},
			},
		}
		if got := resolveBuiltin("myshell", cfg); got != "shell" {
			t.Errorf("got %q, want shell", got)
		}
	})
}
