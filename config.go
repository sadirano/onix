package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the on-disk representation of ~/.onix/config.toml.
//
// We keep it deliberately small: today only [[actions]] are configurable
// here. Anything global (editor, shell) still flows through env vars so
// we don't end up with two sources of truth that drift apart.
//
// Schema:
//
//	[[actions]]
//	name = "test"
//	exec = "go"
//	args = ["test", "./..."]
//
//	[[actions]]
//	name = "pr"
//	exec = "gh"
//	args = ["pr", "view", "{extras}", "--web"]
//
// Variables substituted in `args`:
//
//	{target}    resolved alias path (forward-slash form)
//	{alias}     alias name as the user typed it (lowercased)
//	{extras}    additional argv from the caller (variadic when a whole arg)
//
// When no arg in `args` contains `{extras}`, extras are appended to argv
// — the most common case. When at least one arg contains it, extras are
// spliced in at that position instead of appended.
type Config struct {
	Actions []Action `toml:"actions"`
}

// Action declares one custom command wrapper. The combination of name +
// exec + args is enough to generate both a shell function and an argv
// at runtime. We validate names and exec before saving / running so
// errors fire early, not at the first `onix exec` invocation.
type Action struct {
	Name string   `toml:"name"`
	Exec string   `toml:"exec"`
	Args []string `toml:"args"`
}

// LoadConfig reads ~/.onix/config.toml. A missing file is fine — it just
// means "no custom actions declared." Bad TOML or schema violations are
// reported with a path-and-message error so the user can find the file
// they need to fix.
func LoadConfig(home string) (*Config, error) {
	p := configPath(home)
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
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return cfg, nil
}

// validate enforces the schema invariants the rest of the code depends on:
// every action has a name and an exec, names are unique, and names contain
// no characters that would break shell-function generation. We're strict
// here because the alternative is a cryptic PowerShell parse error several
// commands later.
func (c *Config) validate() error {
	seen := map[string]struct{}{}
	for i, a := range c.Actions {
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("[[actions]][%d]: name is required", i)
		}
		if strings.TrimSpace(a.Exec) == "" {
			return fmt.Errorf("[[actions]][%d] %s: exec is required", i, a.Name)
		}
		if !validActionName(a.Name) {
			return fmt.Errorf("[[actions]][%d] %s: name must be [A-Za-z0-9_-]+ (got %q)", i, a.Name, a.Name)
		}
		key := strings.ToLower(a.Name)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("[[actions]][%d]: duplicate action name %q", i, a.Name)
		}
		seen[key] = struct{}{}
		// Built-in shadow check — refuse names that would collide with the
		// built-in shell functions, otherwise the user's intent silently loses.
		if isBuiltinName(key) {
			return fmt.Errorf("[[actions]][%d]: name %q shadows a built-in action", i, a.Name)
		}
	}
	return nil
}

// FindAction returns the action with the given name (case-insensitive) or
// nil. Lowercased lookup matches our convention everywhere else.
func (c *Config) FindAction(name string) *Action {
	key := strings.ToLower(strings.TrimSpace(name))
	for i := range c.Actions {
		if strings.ToLower(c.Actions[i].Name) == key {
			return &c.Actions[i]
		}
	}
	return nil
}

// ExpandAction substitutes {target}, {alias}, {extras} in a.Args and
// returns the full argv to exec (a.Exec prepended).
//
// {extras} as a whole arg splices in the extras list element-by-element,
// preserving each as its own argv slot. {extras} as a substring is joined
// with spaces — useful for forms like "--filter={extras}" but rarely the
// shape the user wants. When neither shape is present, extras append at
// the end, which is the most common case.
func ExpandAction(a *Action, target, alias string, extras []string) []string {
	argv := make([]string, 0, len(a.Args)+len(extras)+1)
	argv = append(argv, a.Exec)

	seenExtras := false
	joinedExtras := strings.Join(extras, " ")

	for _, raw := range a.Args {
		switch {
		case raw == "{extras}":
			// Variadic splice. We add each extra as its own argv element
			// so flags like -v keep their identity through exec.
			argv = append(argv, extras...)
			seenExtras = true
		case strings.Contains(raw, "{extras}"):
			// Substring substitution. Join with space because there's no
			// sensible way to splice multiple argv elements into a single
			// string slot.
			argv = append(argv,
				strings.ReplaceAll(
					substituteSingles(raw, target, alias),
					"{extras}", joinedExtras))
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

// substituteSingles replaces the simple {target} and {alias} tokens inside
// one arg. Kept separate from {extras} handling so the variadic case can
// short-circuit without double work.
func substituteSingles(s, target, alias string) string {
	if !strings.Contains(s, "{") {
		return s
	}
	s = strings.ReplaceAll(s, "{target}", target)
	s = strings.ReplaceAll(s, "{alias}", alias)
	return s
}

// validActionName mirrors the character class we use to generate the
// PowerShell function name. Anything outside [A-Za-z0-9_-] would either
// be invalid in PowerShell or require quoting we'd rather not emit.
func validActionName(s string) bool {
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

// isBuiltinName reports whether name collides with a generated built-in
// shell function. Updated when we add new built-ins; the list is small
// enough that a switch is clearer than a map.
func isBuiltinName(lower string) bool {
	switch lower {
	case "o", "n", "s", "y", "r":
		return true
	}
	return false
}
