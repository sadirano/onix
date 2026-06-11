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
	Grep      Grep              `toml:"grep"`
	Picker    Picker            `toml:"picker"`
}

// Picker tunes the unknown-alias directory picker (register.cmd: Everything
// `es` piped into fzf). Exclude lists path fragments filtered out of the es
// results as `!path:<fragment>` query terms, so dependency/cache trees
// don't drown the real candidates — and so the -n result cap is spent on
// directories worth picking. A nil list (key absent) applies
// PickerExcludeDefaults; an explicit `exclude = []` disables filtering.
// ExcludeExtra extends (rather than replaces) whatever Exclude resolves
// to — the place for machine-specific noise the shipped defaults
// shouldn't carry.
type Picker struct {
	Exclude      []string `toml:"exclude"`
	ExcludeExtra []string `toml:"exclude_extra"`
}

// PickerExcludeDefaults returns a fresh copy of the default exclusion
// fragments: dependency/build/cache trees, hidden-by-convention folder
// prefixes, and the Windows system trees (Windows, Program Files,
// ProgramData, AppData) nobody registers an alias into. Only trees
// common to most Windows machines belong here — machine-specific noise
// (game stores, content libraries) goes in exclude_extra or the swept
// file. Fragments are matched as substrings of the full path; ones with
// spaces are emitted quoted, which es accepts as long as the fragment
// has no trailing backslash. es treats `$`, `.`, `_`, and `[` as
// literals here.
func PickerExcludeDefaults() []string {
	return []string{
		// any path component starting with '.', '_', or '[' — covers
		// .git/.venv/.cargo/.gradle, __pycache__/__tests__/__snapshots__,
		// and bracket-tagged release folders
		`\.`,
		`\_`,
		`\[`,
		// dependency / build / cache trees (\test is a prefix: it also
		// catches tests/testing/testdata)
		`node_modules`,
		`go\pkg\mod`,
		`site-packages`,
		`\cache\`,
		`\caches\`,
		`\temp\`,
		`\lib\`,
		`\libs\`,
		`\libraries\`,
		`\src\`,
		`\bin\`,
		`\obj\`,
		`\build\`,
		`\dist\`,
		`\x64\`,
		`\x86\`,
		`\Debug\`,
		`\Release\`,
		`\modules\`,
		`\intermediates\`,
		`\packages\`,
		`\versions\`,
		`\test`,
		`\share\`,
		`\locale\`,
		// Windows system trees and browser profiles
		`C:\Windows\`,
		`C:\ProgramData\`,
		`C:\Program Files`,
		`System Volume Information`,
		`$RECYCLE.BIN`,
		`\AppData\`,
		`\User Data`,
		// store / package-manager owned install trees
		`\scoop\apps\`,
		`\steamapps\`,
	}
}

// ExcludeOrDefault distinguishes "key absent" (nil → defaults) from an
// explicit empty list (no filtering).
func (p Picker) ExcludeOrDefault() []string {
	if p.Exclude == nil {
		return PickerExcludeDefaults()
	}
	return p.Exclude
}

// SweptPath returns home/picker.swept — the machine-generated exclusion
// list maintained by `onix --sweep`. One fragment per line; blank lines
// and #-comments are ignored.
func SweptPath(home string) string {
	return filepath.Join(home, "picker.swept")
}

// LoadSwept reads the swept-exclusion file. A missing file is an empty
// list, not an error. Fragments are validated like config excludes so a
// hand-edited bad line fails loudly at sync time instead of silently
// corrupting the generated es query.
func LoadSwept(home string) ([]string, error) {
	data, err := os.ReadFile(SweptPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		frag := strings.TrimSpace(line)
		if frag == "" || strings.HasPrefix(frag, "#") {
			continue
		}
		if err := validateExcludeFragment(frag); err != nil {
			return nil, fmt.Errorf("%s: %w", SweptPath(home), err)
		}
		out = append(out, frag)
	}
	return out, nil
}

// AppendSwept adds fragments to the swept file, skipping ones already
// present (case-insensitive — es matches case-insensitively anyway).
// Returns the fragments actually added.
func AppendSwept(home string, frags []string) ([]string, error) {
	existing, err := LoadSwept(home)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(existing))
	for _, f := range existing {
		seen[strings.ToLower(f)] = struct{}{}
	}
	var added []string
	var b strings.Builder
	for _, f := range frags {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if err := validateExcludeFragment(f); err != nil {
			return nil, err
		}
		if _, dup := seen[strings.ToLower(f)]; dup {
			continue
		}
		seen[strings.ToLower(f)] = struct{}{}
		added = append(added, f)
		b.WriteString(f)
		b.WriteString("\n")
	}
	if len(added) == 0 {
		return nil, nil
	}
	fh, err := os.OpenFile(SweptPath(home), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if _, err := fh.WriteString(b.String()); err != nil {
		return nil, err
	}
	return added, nil
}

// PickerExcludes composes the full exclusion list the generated
// register.cmd should carry: exclude (or the defaults), plus
// exclude_extra, plus the swept file, deduplicated case-insensitively
// in that order.
func PickerExcludes(home string, c *Config) ([]string, error) {
	swept, err := LoadSwept(home)
	if err != nil {
		return nil, err
	}
	merged := c.Picker.ExcludeOrDefault()
	merged = append(merged, c.Picker.ExcludeExtra...)
	merged = append(merged, swept...)
	seen := make(map[string]struct{}, len(merged))
	out := merged[:0]
	for _, f := range merged {
		if _, dup := seen[strings.ToLower(f)]; dup {
			continue
		}
		seen[strings.ToLower(f)] = struct{}{}
		out = append(out, f)
	}
	return out, nil
}

// Grep tunes the `sg` (grep) command. Every field has a built-in
// default — empty values fall through. FzfColors layers extra --color
// flags on top of FZF_DEFAULT_OPTS; leave it empty to let the theme
// (or the user's env) speak for itself. RgColors is a list of rg's
// --colors specs (e.g. "path:fg:blue") that get passed verbatim.
// LiteralNonASCII keeps non-ASCII characters in the query verbatim;
// otherwise they're rewritten to the regex "." so a UTF-8 query like
// "café" still matches the same bytes in cp1252 files (and any other
// encoding) at the cost of accepting one extra char in that position.
type Grep struct {
	PreviewWindow   string   `toml:"preview_window"`
	PreviewCommand  string   `toml:"preview_command"`
	FzfColors       string   `toml:"fzf_colors"`
	RgColors        []string `toml:"rg_colors"`
	LiteralNonASCII bool     `toml:"literal_non_ascii"`
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
			return fmt.Errorf("shortcut %q: invalid built-in name (must be one of: o, e, s, y, p, r, sg, ff)", name)
		}
		seen[strings.ToLower(shortcut)] = struct{}{}
	}
	for _, frag := range c.Picker.Exclude {
		if err := validateExcludeFragment(frag); err != nil {
			return fmt.Errorf("picker exclude: %w", err)
		}
	}
	for _, frag := range c.Picker.ExcludeExtra {
		if err := validateExcludeFragment(frag); err != nil {
			return fmt.Errorf("picker exclude_extra: %w", err)
		}
	}
	return nil
}

// validateExcludeFragment enforces the constraints the generated
// register.cmd imposes on es query terms: a quote would break the batch
// line's tokenising, and a spaced fragment is emitted quoted, where a
// trailing backslash right before the closing quote is eaten by es's
// arg parsing.
func validateExcludeFragment(frag string) error {
	if strings.ContainsAny(frag, `"`) {
		return fmt.Errorf("fragment %q cannot contain double quotes", frag)
	}
	if strings.TrimSpace(frag) == "" {
		return fmt.Errorf("empty fragment")
	}
	if strings.ContainsAny(frag, " \t") && strings.HasSuffix(frag, `\`) {
		return fmt.Errorf("fragment %q with spaces cannot end with a backslash", frag)
	}
	return nil
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
	"p":  "p",
	"r":  "r",
	"sg": "sg",
	"ff": "ff",
}

func IsBuiltinName(lower string) bool {
	_, ok := builtinDefaults[lower]
	return ok
}
