package config

import (
	"os"
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

func TestLoadLua(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	onixDir := filepath.Join(dir, ".onix")
	if err := os.MkdirAll(onixDir, 0o755); err != nil {
		t.Fatal(err)
	}

	lua := `return {
  settings = {
    editor      = "code",
    timing      = true,
    debug       = false,
    disable_run = true,
  },
  context = {
    source   = "env",
    var      = "MY_CTX",
    template = "ctx/{value}",
  },
  actions = {
    { name = "ed", builtin = "editor" },
    { name = "sh", builtin = "shell" },
  },
  modules = {
    {
      name    = "mymod",
      repo    = "user/mymod",
      ref     = "main",
      enabled = true,
      config  = { key = "val", num = 42 },
    },
    {
      repo    = "user/other",
      enabled = false,
    },
  },
}`
	if err := os.WriteFile(filepath.Join(onixDir, "config.lua"), []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Settings.Editor != "code" {
		t.Errorf("Editor = %q, want %q", cfg.Settings.Editor, "code")
	}
	if !cfg.Settings.Timing {
		t.Error("Timing should be true")
	}
	if cfg.Settings.Debug {
		t.Error("Debug should be false")
	}
	if !cfg.Settings.DisableRun {
		t.Error("DisableRun should be true")
	}

	if cfg.Context.Source != "env" {
		t.Errorf("Context.Source = %q, want %q", cfg.Context.Source, "env")
	}
	if cfg.Context.Var != "MY_CTX" {
		t.Errorf("Context.Var = %q, want %q", cfg.Context.Var, "MY_CTX")
	}
	if cfg.Context.Template != "ctx/{value}" {
		t.Errorf("Context.Template = %q, want %q", cfg.Context.Template, "ctx/{value}")
	}

	if len(cfg.Actions) != 2 {
		t.Fatalf("len(Actions) = %d, want 2", len(cfg.Actions))
	}
	if cfg.Actions[0].Name != "ed" || cfg.Actions[0].Builtin != "editor" {
		t.Errorf("Actions[0] = %+v", cfg.Actions[0])
	}

	if len(cfg.Modules) != 2 {
		t.Fatalf("len(Modules) = %d, want 2", len(cfg.Modules))
	}
	m0 := cfg.Modules[0]
	if m0.Name != "mymod" || m0.Repo != "user/mymod" || m0.Ref != "main" || !m0.Enabled {
		t.Errorf("Modules[0] = %+v", m0)
	}
	if m0.Config["key"] != "val" {
		t.Errorf("Modules[0].Config[key] = %v, want %q", m0.Config["key"], "val")
	}
	if m0.Config["num"] != int64(42) {
		t.Errorf("Modules[0].Config[num] = %v, want 42", m0.Config["num"])
	}
	m1 := cfg.Modules[1]
	if m1.Repo != "user/other" || m1.Enabled {
		t.Errorf("Modules[1] = %+v", m1)
	}
}

func TestLoadLuaMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	// No config.lua written — should return empty config.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
}

func TestSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	orig := &Config{
		Settings: Settings{
			Editor:     "vim",
			Timing:     true,
			DisableRun: true,
		},
		Context: ContextConfig{
			Source:   "env",
			Var:      "CTX",
			Template: "ctx/{value}",
		},
		Actions: []Action{
			{Name: "ed", Builtin: "editor"},
		},
		Modules: []Module{
			{
				Name:    "mod1",
				Repo:    "user/mod1",
				Ref:     "main",
				Enabled: true,
				Config:  map[string]any{"x": "y"},
			},
			{
				Repo:    "user/mod2",
				Enabled: false,
			},
		},
	}

	if err := Save(orig); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() after Save() error: %v", err)
	}

	if got.Settings.Editor != orig.Settings.Editor {
		t.Errorf("Editor: got %q, want %q", got.Settings.Editor, orig.Settings.Editor)
	}
	if got.Settings.Timing != orig.Settings.Timing {
		t.Errorf("Timing: got %v, want %v", got.Settings.Timing, orig.Settings.Timing)
	}
	if got.Context.Source != orig.Context.Source {
		t.Errorf("Context.Source: got %q, want %q", got.Context.Source, orig.Context.Source)
	}
	if len(got.Actions) != len(orig.Actions) {
		t.Fatalf("len(Actions): got %d, want %d", len(got.Actions), len(orig.Actions))
	}
	if len(got.Modules) != len(orig.Modules) {
		t.Fatalf("len(Modules): got %d, want %d", len(got.Modules), len(orig.Modules))
	}
	if got.Modules[0].Name != orig.Modules[0].Name {
		t.Errorf("Modules[0].Name: got %q, want %q", got.Modules[0].Name, orig.Modules[0].Name)
	}
	if got.Modules[0].Config["x"] != orig.Modules[0].Config["x"] {
		t.Errorf("Modules[0].Config[x]: got %v, want %v", got.Modules[0].Config["x"], orig.Modules[0].Config["x"])
	}
	if got.Modules[1].Enabled {
		t.Error("Modules[1].Enabled should be false")
	}
}
