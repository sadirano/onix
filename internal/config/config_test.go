package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestConfig_LoadMissingReturnsEmpty is the first-run path: no config
// file means "no custom actions", not an error.
func TestConfig_LoadMissingReturnsEmpty(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil empty config, got nil")
	}
}

// TestGrep_Defaults checks that the *OrDefault knobs fall through when
// empty/whitespace and pass overrides through verbatim. FzfColors has
// no default — it's consumed raw by search.go.
func TestGrep_Defaults(t *testing.T) {
	type probe struct {
		name string
		get  func(Grep) string
		set  func(*Grep, string)
		def  string
	}
	probes := []probe{
		{
			"preview_window",
			func(g Grep) string { return g.PreviewWindowOrDefault() },
			func(g *Grep, v string) { g.PreviewWindow = v },
			GrepPreviewWindowDefault,
		},
		{
			"preview_command",
			func(g Grep) string { return g.PreviewCommandOrDefault() },
			func(g *Grep, v string) { g.PreviewCommand = v },
			GrepPreviewCommandDefault,
		},
	}
	for _, p := range probes {
		t.Run(p.name+"/empty falls back", func(t *testing.T) {
			if got := p.get(Grep{}); got != p.def {
				t.Errorf("got %q, want default %q", got, p.def)
			}
		})
		t.Run(p.name+"/whitespace falls back", func(t *testing.T) {
			g := Grep{}
			p.set(&g, "   ")
			if got := p.get(g); got != p.def {
				t.Errorf("got %q, want default %q", got, p.def)
			}
		})
		t.Run(p.name+"/override passes through", func(t *testing.T) {
			g := Grep{}
			p.set(&g, "OVERRIDE")
			if got := p.get(g); got != "OVERRIDE" {
				t.Errorf("got %q, want override", got)
			}
		})
	}
}

// TestGrep_RgColorsOrDefault: empty or nil falls through to defaults;
// a non-empty user list passes through verbatim.
func TestGrep_RgColorsOrDefault(t *testing.T) {
	t.Run("nil falls back", func(t *testing.T) {
		got := (Grep{}).RgColorsOrDefault()
		if !reflect.DeepEqual(got, GrepRgColorsDefault()) {
			t.Errorf("got %v, want defaults %v", got, GrepRgColorsDefault())
		}
	})
	t.Run("empty falls back", func(t *testing.T) {
		got := (Grep{RgColors: []string{}}).RgColorsOrDefault()
		if !reflect.DeepEqual(got, GrepRgColorsDefault()) {
			t.Errorf("got %v, want defaults", got)
		}
	})
	t.Run("override passes through", func(t *testing.T) {
		want := []string{"match:fg:yellow"}
		got := (Grep{RgColors: want}).RgColorsOrDefault()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// TestGrep_DefaultRgColorsIsolated guards against a caller mutating
// the slice the default-helper returns and corrupting future calls.
func TestGrep_DefaultRgColorsIsolated(t *testing.T) {
	first := GrepRgColorsDefault()
	first[0] = "tampered"
	second := GrepRgColorsDefault()
	if second[0] == "tampered" {
		t.Errorf("default leaked: %v", second)
	}
}

// TestConfig_GrepRoundTrip checks the [grep] section parses cleanly.
func TestConfig_GrepRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `
[grep]
preview_window = "right:50%"
preview_command = "bat {1}"
fzf_colors = "hl:red"
rg_colors = ["match:fg:yellow", "path:fg:cyan"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Grep.PreviewWindow != "right:50%" ||
		cfg.Grep.PreviewCommand != "bat {1}" ||
		cfg.Grep.FzfColors != "hl:red" ||
		!reflect.DeepEqual(cfg.Grep.RgColors, []string{"match:fg:yellow", "path:fg:cyan"}) {
		t.Errorf("Grep round-trip mismatch: %+v", cfg.Grep)
	}
}

func TestBuiltinDefaults(t *testing.T) {
	m := BuiltinDefaults()
	if len(m) == 0 {
		t.Error("BuiltinDefaults returned no entries")
	}
	if m["o"] != "o" {
		t.Errorf("builtin 'o' missing or wrong: %q", m["o"])
	}
}

func TestPicker_ExcludeOrDefault(t *testing.T) {
	// Absent key: built-in defaults apply.
	if got := (Picker{}).ExcludeOrDefault(); len(got) == 0 {
		t.Error("nil exclude list should fall back to defaults")
	}
	// Explicit empty list: filtering deliberately off.
	if got := (Picker{Exclude: []string{}}).ExcludeOrDefault(); len(got) != 0 {
		t.Errorf("explicit empty list must disable filtering, got %v", got)
	}
	// User list replaces the defaults entirely.
	got := (Picker{Exclude: []string{`\test\`}}).ExcludeOrDefault()
	if len(got) != 1 || got[0] != `\test\` {
		t.Errorf("user list not honoured: %v", got)
	}
}

func TestValidate_PickerExclude(t *testing.T) {
	bad := &Config{Picker: Picker{Exclude: []string{`with"quote`}}}
	if err := bad.Validate(); err == nil {
		t.Error("quoted fragment must fail validation (breaks batch tokenising)")
	}
	empty := &Config{Picker: Picker{Exclude: []string{"  "}}}
	if err := empty.Validate(); err == nil {
		t.Error("blank fragment must fail validation")
	}
	ok := &Config{Picker: Picker{Exclude: []string{`node_modules`, `\.git\`}}}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid fragments rejected: %v", err)
	}
}
