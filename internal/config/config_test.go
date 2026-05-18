package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestConfig_RoundTrip writes a TOML config by hand, reads it via
// LoadConfig, and verifies every field arrives intact. Doing the write
// by hand (rather than encoding the struct) catches regressions where
// our schema diverges from what users actually write in their files.
func TestConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `
[[actions]]
name = "test"
exec = "go"
args = ["test", "./..."]

[[actions]]
name = "pr"
exec = "gh"
args = ["pr", "view", "{extras}", "--web"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(cfg.Actions); got != 2 {
		t.Fatalf("len(Actions) = %d, want 2", got)
	}
	a := cfg.FindAction("test")
	if a == nil {
		t.Fatal("FindAction(test) returned nil")
	}
	if a.Exec != "go" || !reflect.DeepEqual(a.Args, []string{"test", "./..."}) {
		t.Errorf("test action mismatch: exec=%q args=%v", a.Exec, a.Args)
	}
}

// TestConfig_LoadMissingReturnsEmpty is the first-run path: no config
// file means "no custom actions", not an error. The rest of the binary
// relies on Config never being nil.
func TestConfig_LoadMissingReturnsEmpty(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg == nil || len(cfg.Actions) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

// TestConfig_ValidateRejectsBadShape locks in the validation contract.
// Each row is one violation we expect to surface as a clear error.
func TestConfig_ValidateRejectsBadShape(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing name",
			body: `[[actions]]
exec = "go"`,
			want: "name is required",
		},
		{
			name: "missing exec",
			body: `[[actions]]
name = "foo"`,
			want: "exec is required",
		},
		{
			name: "duplicate name",
			body: `[[actions]]
name = "foo"
exec = "x"

[[actions]]
name = "foo"
exec = "y"`,
			want: "duplicate action name",
		},
		{
			name: "shadows builtin",
			body: `[[actions]]
name = "o"
exec = "x"`,
			want: "shadows a built-in",
		},
		{
			name: "bad name characters",
			body: `[[actions]]
name = "foo bar"
exec = "x"`,
			want: "name must be",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(dir)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
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

// TestConfig_GrepRoundTrip checks the [grep] section parses cleanly
// alongside [[actions]] and that every knob survives a load.
func TestConfig_GrepRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `
[grep]
preview_window = "right:50%"
preview_command = "bat {1}"
fzf_colors = "hl:red"

[[actions]]
name = "t"
exec = "go"
args = ["test"]
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
		cfg.Grep.FzfColors != "hl:red" {
		t.Errorf("Grep round-trip mismatch: %+v", cfg.Grep)
	}
}

// TestExpandAction covers the substitution rules. We pick cases that hit
// each branch: target/alias substring, {extras} as a whole arg (variadic),
// {extras} as substring (joined), and no-extras append.
func TestExpandAction(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		target string
		alias  string
		extras []string
		want   []string
	}{
		{
			name:   "no template, extras appended",
			action: Action{Exec: "go", Args: []string{"test", "./..."}},
			extras: []string{"-v"},
			want:   []string{"go", "test", "./...", "-v"},
		},
		{
			name:   "no template, no extras",
			action: Action{Exec: "go", Args: []string{"test", "./..."}},
			want:   []string{"go", "test", "./..."},
		},
		{
			name:   "{extras} as whole arg splices",
			action: Action{Exec: "gh", Args: []string{"pr", "view", "{extras}", "--web"}},
			extras: []string{"42", "--state", "open"},
			want:   []string{"gh", "pr", "view", "42", "--state", "open", "--web"},
		},
		{
			name:   "{extras} substring joins with space",
			action: Action{Exec: "rg", Args: []string{"--glob={extras}"}},
			extras: []string{"*.go", "src/"},
			want:   []string{"rg", "--glob=*.go src/"},
		},
		{
			name:   "target and alias substitution",
			action: Action{Exec: "echo", Args: []string{"{alias}@{target}"}},
			target: "C:/projects/acme",
			alias:  "acme",
			want:   []string{"echo", "acme@C:/projects/acme"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandAction(&tc.action, tc.target, tc.alias, tc.extras)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExpandAction = %v, want %v", got, tc.want)
			}
		})
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

func TestFindAction_Missing(t *testing.T) {
	cfg := &Config{}
	if cfg.FindAction("nope") != nil {
		t.Error("FindAction found a non-existent action")
	}
}
