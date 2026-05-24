package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the on-disk representation of ~/.onix/config.toml.
type Config struct {
	Shortcuts map[string]string `toml:"shortcuts,omitempty"`
	Actions   []Action          `toml:"actions"`
	Grep      Grep              `toml:"grep"`
}

// Action declares one custom command wrapper.
type Action struct {
	Name string   `toml:"name"`
	Exec string   `toml:"exec"`
	Args []string `toml:"args"`
}

// Grep tunes the `sg` (grep) command. Every field has a built-in
// default — empty values fall through. FzfColors layers extra --color
// flags on top of FZF_DEFAULT_OPTS; leave it empty to let the theme
// (or the user's env) speak for itself. RgColors is a list of rg's
// --colors specs (e.g. "path:fg:blue") that get passed verbatim.
type Grep struct {
	PreviewWindow  string   `toml:"preview_window"`
	PreviewCommand string   `toml:"preview_command"`
	FzfColors      string   `toml:"fzf_colors"`
	RgColors       []string `toml:"rg_colors"`
}

// Defaults for the [grep] section. The preview-window value scrolls
// the preview to the match line ({2} = rg's line number, +3 keeps it
// off the top edge, /3 reserves a third of the pane below it) and
// freezes three header lines — bat's grid style wraps the "File:"
// banner in top and bottom rules, so the frozen region is rule +
// banner + rule.
const (
	GrepPreviewWindowDefault  = "up:60%:border-bottom:+{2}+3/3:~3"
	GrepPreviewCommandDefault = "bat --style=numbers,header,grid --color=always {1} --highlight-line {2}"
)

// GrepRgColorsDefault returns a fresh copy of the default --colors
// specs so callers can't mutate the package-level slice.
func GrepRgColorsDefault() []string {
	return []string{
		"path:fg:blue",
		"line:fg:green",
		"match:fg:red",
		"match:style:bold",
	}
}

func (g Grep) PreviewWindowOrDefault() string {
	if strings.TrimSpace(g.PreviewWindow) != "" {
		return g.PreviewWindow
	}
	return GrepPreviewWindowDefault
}

func (g Grep) PreviewCommandOrDefault() string {
	if strings.TrimSpace(g.PreviewCommand) != "" {
		return g.PreviewCommand
	}
	return GrepPreviewCommandDefault
}

func (g Grep) RgColorsOrDefault() []string {
	if len(g.RgColors) > 0 {
		return g.RgColors
	}
	return GrepRgColorsDefault()
}

// Path returns home/config.toml.
func Path(home string) string {
	return filepath.Join(home, "config.toml")
}

// LoadConfig reads ~/.onix/config.toml.
func LoadConfig(home string) (*Config, error) {
	p := Path(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	cfg := &Config{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return cfg, nil
}

// Validate enforces the schema invariants.
func (c *Config) Validate() error {
	seen := map[string]struct{}{}
	for name, shortcut := range c.Shortcuts {
		if !ValidActionName(shortcut) {
			return fmt.Errorf("shortcut %q: name must be [A-Za-z0-9_-]+ (got %q)", name, shortcut)
		}
		if _, ok := builtinDefaults[name]; !ok {
			return fmt.Errorf("shortcut %q: invalid built-in name (must be one of: o, e, s, y, r, sg, ff)", name)
		}
		seen[strings.ToLower(shortcut)] = struct{}{}
	}

	for i, a := range c.Actions {
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("[[actions]][%d]: name is required", i)
		}
		if strings.TrimSpace(a.Exec) == "" {
			return fmt.Errorf("[[actions]][%d] %s: exec is required", i, a.Name)
		}
		if !ValidActionName(a.Name) {
			return fmt.Errorf("[[actions]][%d] %s: name must be [A-Za-z0-9_-]+ (got %q)", i, a.Name, a.Name)
		}
		key := strings.ToLower(a.Name)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("[[actions]][%d]: duplicate action name %q (possibly shadowing a shortcut)", i, a.Name)
		}
		seen[key] = struct{}{}
		if IsBuiltinName(key) {
			return fmt.Errorf("[[actions]][%d]: name %q shadows a built-in action", i, a.Name)
		}
	}
	return nil
}

// FindAction returns the action with the given name (case-insensitive) or nil.
func (c *Config) FindAction(name string) *Action {
	key := strings.ToLower(strings.TrimSpace(name))
	for i := range c.Actions {
		if strings.ToLower(c.Actions[i].Name) == key {
			return &c.Actions[i]
		}
	}
	return nil
}

// ExpandAction substitutes {target}, {alias}, {extras} in a.Args.
func ExpandAction(a *Action, target, alias string, extras []string) []string {
	argv := make([]string, 0, len(a.Args)+len(extras)+1)
	argv = append(argv, a.Exec)

	seenExtras := false
	joinedExtras := strings.Join(extras, " ")

	for _, raw := range a.Args {
		switch {
		case raw == "{extras}":
			argv = append(argv, extras...)
			seenExtras = true
		case strings.Contains(raw, "{extras}"):
			argv = append(argv,
				strings.ReplaceAll(
					substituteSingles(raw, target, alias),
					"{extras}", joinedExtras,
				))
			seenExtras = true
		default:
			argv = append(argv, substituteSingles(raw, target, alias))
		}
	}

	if !seenExtras && len(extras) > 0 {
		argv = append(argv, extras...)
	}
	return argv
}

func substituteSingles(s, target, alias string) string {
	if !strings.Contains(s, "{") {
		return s
	}
	s = strings.ReplaceAll(s, "{target}", target)
	s = strings.ReplaceAll(s, "{alias}", alias)
	return s
}

func ValidActionName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// BuiltinDefaults returns the map of default shortcut names.
func BuiltinDefaults() map[string]string {
	out := make(map[string]string, len(builtinDefaults))
	for k, v := range builtinDefaults {
		out[k] = v
	}
	return out
}

var builtinDefaults = map[string]string{
	"o":  "o",
	"e":  "e",
	"s":  "s",
	"y":  "y",
	"r":  "r",
	"sg": "sg",
	"ff": "ff",
}

func IsBuiltinName(lower string) bool {
	_, ok := builtinDefaults[lower]
	return ok
}
