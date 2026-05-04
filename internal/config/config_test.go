package config

import (
	"encoding/json"
	"testing"
)

func TestEffectiveName(t *testing.T) {
	tests := []struct {
		name string
		m    Module
		want string
	}{
		{"explicit name used", Module{Name: "sg", Repo: "sadirano/onix-sg"}, "sg"},
		{"inferred from repo", Module{Repo: "sadirano/onix-sg"}, "onix-sg"},
		{"inferred from bare repo", Module{Repo: "onix-sg"}, "onix-sg"},
		{"name overrides repo", Module{Name: "mysearch", Repo: "vendor/onix-sg"}, "mysearch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.EffectiveName(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigJSON(t *testing.T) {
	t.Run("nil map returns empty object", func(t *testing.T) {
		m := Module{}
		if got := m.ConfigJSON(); got != "{}" {
			t.Errorf("got %q, want {}", got)
		}
	})
	t.Run("empty map returns empty object", func(t *testing.T) {
		m := Module{Config: map[string]any{}}
		if got := m.ConfigJSON(); got != "{}" {
			t.Errorf("got %q, want {}", got)
		}
	})
	t.Run("non-empty map produces valid JSON", func(t *testing.T) {
		m := Module{Config: map[string]any{"flag": "--type go", "depth": 3}}
		got := m.ConfigJSON()
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("invalid JSON %q: %v", got, err)
		}
		if parsed["flag"] != "--type go" {
			t.Errorf("flag: got %v, want %q", parsed["flag"], "--type go")
		}
	})
}

func TestIsDebugEnabled(t *testing.T) {
	tests := []struct {
		name          string
		settingsDebug bool
		onixDebug     string
		omniDebug     string
		want          bool
	}{
		{"all off", false, "", "", false},
		{"Settings.Debug true", true, "", "", true},
		{"ONIX_DEBUG=1", false, "1", "", true},
		{"OMNI_DEBUG=1", false, "", "1", true},
		{"ONIX_DEBUG not 1", false, "true", "", false},
		{"ONIX_DEBUG=0", false, "0", "", false},
		{"both env vars set", false, "1", "1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ONIX_DEBUG", tt.onixDebug)
			t.Setenv("OMNI_DEBUG", tt.omniDebug)
			cfg := &Config{Settings: Settings{Debug: tt.settingsDebug}}
			if got := cfg.IsDebugEnabled(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveEditor(t *testing.T) {
	tests := []struct {
		name       string
		cfgEditor  string
		editor     string
		omniEditor string
		want       string
	}{
		{"config takes priority", "code", "vim", "nano", "code"},
		{"EDITOR fallback", "", "vim", "", "vim"},
		{"OMNI_EDITOR fallback", "", "", "nano", "nano"},
		{"default nvim", "", "", "", "nvim"},
		{"config beats EDITOR", "code", "vim", "", "code"},
		{"EDITOR beats OMNI_EDITOR", "", "vim", "nano", "vim"},
		{"whitespace-only config falls back", "  ", "vim", "", "vim"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EDITOR", tt.editor)
			t.Setenv("OMNI_EDITOR", tt.omniEditor)
			cfg := &Config{Settings: Settings{Editor: tt.cfgEditor}}
			if got := cfg.ResolveEditor(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindAction(t *testing.T) {
	t.Run("finds by name in declared actions", func(t *testing.T) {
		cfg := &Config{
			Actions: []Action{
				{Name: "myeditor", Builtin: "editor"},
			},
		}
		a := cfg.FindAction("myeditor")
		if a == nil || a.Builtin != "editor" {
			t.Errorf("expected editor builtin, got %v", a)
		}
	})
	t.Run("case-insensitive match", func(t *testing.T) {
		cfg := &Config{
			Actions: []Action{
				{Name: "Shell", Builtin: "shell"},
			},
		}
		if cfg.FindAction("shell") == nil {
			t.Error("expected case-insensitive match")
		}
	})
	t.Run("falls back to DefaultActions when no actions declared", func(t *testing.T) {
		cfg := &Config{}
		a := cfg.FindAction("editor")
		if a == nil || a.Builtin != "editor" {
			t.Errorf("expected default editor action, got %v", a)
		}
	})
	t.Run("all DefaultActions are findable", func(t *testing.T) {
		cfg := &Config{}
		for _, da := range DefaultActions {
			if cfg.FindAction(da.Name) == nil {
				t.Errorf("default action %q not found", da.Name)
			}
		}
	})
	t.Run("returns nil for unknown action", func(t *testing.T) {
		cfg := &Config{}
		if cfg.FindAction("nosuchaction") != nil {
			t.Error("expected nil for unknown action")
		}
	})
	t.Run("declared actions shadow DefaultActions", func(t *testing.T) {
		cfg := &Config{
			Actions: []Action{
				{Name: "editor", Builtin: "shell"}, // override: editor now opens shell
			},
		}
		a := cfg.FindAction("editor")
		if a == nil || a.Builtin != "shell" {
			t.Errorf("expected overridden builtin shell, got %v", a)
		}
	})
}

func TestActionLoadSaveRoundtrip(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())

	orig := &Config{
		Actions: []Action{
			{Name: "myshell", Builtin: "shell"},
			{Name: "myeditor", Builtin: "editor"},
		},
	}
	if err := Save(orig); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(loaded.Actions))
	}
	if loaded.Actions[0].Name != "myshell" || loaded.Actions[0].Builtin != "shell" {
		t.Errorf("action[0]: got %+v", loaded.Actions[0])
	}
	if loaded.Actions[1].Name != "myeditor" || loaded.Actions[1].Builtin != "editor" {
		t.Errorf("action[1]: got %+v", loaded.Actions[1])
	}
}

func TestFindModule(t *testing.T) {
	cfg := &Config{
		Modules: []Module{
			{Name: "sg", Repo: "sadirano/onix-sg"},
			{Repo: "sadirano/onix-ff"},
		},
	}

	t.Run("finds by name", func(t *testing.T) {
		m := cfg.FindModule("sg")
		if m == nil || m.Repo != "sadirano/onix-sg" {
			t.Errorf("expected onix-sg module, got %v", m)
		}
	})
	t.Run("case-insensitive", func(t *testing.T) {
		if cfg.FindModule("SG") == nil {
			t.Error("expected case-insensitive match for SG")
		}
	})
	t.Run("finds by inferred name", func(t *testing.T) {
		m := cfg.FindModule("onix-ff")
		if m == nil || m.Repo != "sadirano/onix-ff" {
			t.Errorf("expected onix-ff module, got %v", m)
		}
	})
	t.Run("returns nil for unknown", func(t *testing.T) {
		if cfg.FindModule("unknown") != nil {
			t.Error("expected nil for unknown module")
		}
	})
}
