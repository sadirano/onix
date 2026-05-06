package config

import (
	"path/filepath"
	"testing"
)

func TestIsDebugEnabled(t *testing.T) {
	tests := []struct {
		name          string
		settingsDebug bool
		onixDebug     string
		want          bool
	}{
		{"all false", false, "", false},
		{"settings true", true, "", true},
		{"ONIX_DEBUG=1", false, "1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ONIX_DEBUG", tt.onixDebug)
			cfg := &Config{Settings: Settings{Debug: tt.settingsDebug}}
			if got := cfg.IsDebugEnabled(); got != tt.want {
				t.Errorf("IsDebugEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveEditor(t *testing.T) {
	tests := []struct {
		name       string
		settingsEd string
		envEditor  string
		want       string
	}{
		{"default nvim", "", "", "nvim"},
		{"settings beats env", "code", "vim", "code"},
		{"env fallback", "", "vim", "vim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EDITOR", tt.envEditor)
			cfg := &Config{Settings: Settings{Editor: tt.settingsEd}}
			if got := cfg.ResolveEditor(); got != tt.want {
				t.Errorf("ResolveEditor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDir(t *testing.T) {
	t.Run("resolves to .onix in home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("USERPROFILE", home)
		t.Setenv("HOME", home)

		got := Dir()
		want := filepath.Join(home, ".onix")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("panics when home is missing", func(t *testing.T) {
		t.Setenv("USERPROFILE", "")
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
		t.Setenv("HOME", "")

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Dir() should have panicked")
			}
		}()

		_ = Dir()
	})
}

func TestNormalizeRepo(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"user/repo", "user/repo"},
		{"github.com/user/repo", "user/repo"},
		{"https://github.com/user/repo", "user/repo"},
		{"https://github.com/user/repo.git", "user/repo"},
	}
	for _, tt := range tests {
		if got := NormalizeRepo(tt.in); got != tt.want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
