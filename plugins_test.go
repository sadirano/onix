package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlugins_RoundTrip writes a plugins.toml by hand (so we exercise the
// exact shape users will see), reads it back, and walks the data path.
// Includes config map round-trip so the JSON-inline serialiser stays honest.
func TestPlugins_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `
[[plugins]]
name = "tts"
repo = "sadirano/onix-tts"
sha  = "abc123"
config = {rate = 5, voice = "Hazel"}

[[plugins]]
name = "timer"
repo = "sadirano/onix-timer"
sha  = "def456"

[[plugins.entries]]
name = "start"
cmd  = "t-start"

[[plugins.entries]]
name = "stop"
cmd  = "t-stop"
`
	if err := os.WriteFile(filepath.Join(dir, "plugins.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pf, err := LoadPlugins(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pf.Plugins) != 2 {
		t.Fatalf("len(Plugins) = %d, want 2", len(pf.Plugins))
	}

	tts := pf.FindPlugin("tts")
	if tts == nil || tts.Repo != "sadirano/onix-tts" || tts.SHA != "abc123" {
		t.Errorf("tts plugin mismatch: %+v", tts)
	}
	if tts.Config["voice"] != "Hazel" {
		t.Errorf("tts voice = %v, want Hazel", tts.Config["voice"])
	}

	tm := pf.FindPlugin("timer")
	if tm == nil || len(tm.Entries) != 2 {
		t.Fatalf("timer plugin mismatch: %+v", tm)
	}
	if tm.Entries[0].Name != "start" || tm.Entries[0].EffectiveCmd() != "t-start" {
		t.Errorf("timer entry 0 mismatch: %+v", tm.Entries[0])
	}

	// Save round-trip — the rewritten file must reload to the same shape.
	if err := SavePlugins(dir, pf); err != nil {
		t.Fatalf("save: %v", err)
	}
	pf2, err := LoadPlugins(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(pf2.Plugins) != 2 {
		t.Fatalf("after reload len = %d, want 2", len(pf2.Plugins))
	}
	if pf2.FindPlugin("timer").Entries[1].EffectiveCmd() != "t-stop" {
		t.Errorf("entry round-trip lost cmd override")
	}
}

// TestPlugins_LoadMissingReturnsEmpty is the first-run path.
func TestPlugins_LoadMissingReturnsEmpty(t *testing.T) {
	pf, err := LoadPlugins(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pf == nil || len(pf.Plugins) != 0 {
		t.Fatalf("expected empty file, got %+v", pf)
	}
}

// TestValidatePlugins_Collisions locks the hard-error contract: plugin
// wrapper names cannot shadow built-ins, custom actions, or each other.
// Each subtest is one collision shape we want to surface clearly.
func TestValidatePlugins_Collisions(t *testing.T) {
	actions := []Action{{Name: "test", Exec: "go", Args: []string{"test"}}}

	tests := []struct {
		name    string
		plugins []Plugin
		want    string // substring expected in error
	}{
		{
			name:    "shadows builtin o",
			plugins: []Plugin{{Name: "o", Repo: "x/o", SHA: "abc"}},
			want:    "builtin",
		},
		{
			name:    "shadows custom action test",
			plugins: []Plugin{{Name: "test", Repo: "x/y", SHA: "abc"}},
			want:    "action:test",
		},
		{
			name: "duplicate plugin name",
			plugins: []Plugin{
				{Name: "tts", Repo: "x/a", SHA: "abc"},
				{Name: "tts", Repo: "x/b", SHA: "def"},
			},
			want: "plugin:tts",
		},
		{
			name: "entry collides with builtin",
			plugins: []Plugin{{
				Name: "timer", Repo: "x/t", SHA: "abc",
				Entries: []PluginEntry{{Name: "r"}}, // r is built-in
			}},
			want: "builtin",
		},
		{
			name: "missing sha without unpinned",
			plugins: []Plugin{{Name: "p", Repo: "x/p"}},
			want:   "sha is required",
		},
		{
			name:    "bad name characters",
			plugins: []Plugin{{Name: "bad name", Repo: "x/y", SHA: "abc"}},
			want:    "name must be",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePlugins(&PluginsFile{Plugins: tc.plugins}, actions)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestValidatePlugins_HappyPath covers the case the smoke script will hit:
// a real plugin definition with a SHA, no entries, and no collisions.
func TestValidatePlugins_HappyPath(t *testing.T) {
	pf := &PluginsFile{Plugins: []Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc123"},
		{Name: "timer", Repo: "sadirano/onix-timer", SHA: "def456",
			Entries: []PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop", Cmd: "t-stop"},
			}},
	}}
	if err := validatePlugins(pf, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDefaultWrapperName covers the basename-stripping convention.
func TestDefaultWrapperName(t *testing.T) {
	cases := map[string]string{
		"sadirano/onix-tts":           "tts",
		"sadirano/onix-search":        "search",
		"sadirano/onix-timer":         "timer",
		"user/foobar":                 "foobar",
		"https://github.com/x/y.git":  "y",
		"github.com/x/onix-anything":  "anything",
	}
	for in, want := range cases {
		if got := defaultWrapperName(in); got != want {
			t.Errorf("defaultWrapperName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPluginBinaryPath asserts the layout we promise: plugins live under
// home/plugins/<user>/<repo>/<basename>.exe.
func TestPluginBinaryPath(t *testing.T) {
	got := pluginBinaryPath("C:/home", "sadirano/onix-tts")
	want := filepath.Join("C:/home", "plugins", "sadirano", "onix-tts", "onix-tts.exe")
	if got != want {
		t.Errorf("pluginBinaryPath = %q, want %q", got, want)
	}
}
