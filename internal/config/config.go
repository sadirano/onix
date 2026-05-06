package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Entry describes one entry point exposed by a module binary.
// When a module has multiple entry points, each gets its own .cmd wrapper.
type Entry struct {
	Name string `toml:"name"` // sub-command passed to the module binary
	Cmd  string `toml:"cmd"`  // .cmd wrapper filename; defaults to Name
}

// EffectiveCmd returns the .cmd wrapper filename for this entry, falling back
// to Name when Cmd is empty.
func (e Entry) EffectiveCmd() string {
	if e.Cmd != "" {
		return e.Cmd
	}
	return e.Name
}

// Action describes one named command wrapper for the onix binary itself.
// Each action generates a .cmd wrapper in ~/.onix/bin/ that sets ONIX_COMMAND.
type Action struct {
	Name    string `toml:"name"`    // wrapper name (e.g. "editor") and ONIX_COMMAND value
	Builtin string `toml:"builtin"` // which built-in behaviour to run: shell|editor|explorer|print|files|run
}

// ContextConfig defines how to resolve the active working context at runtime.
// When absent from config.toml the zero value is used and HasContext() returns
// false — sub-alias navigation then works without a context layer.
type ContextConfig struct {
	Source   string `toml:"source"`   // "env" (default) | "file" | "cmd" | "alias"
	Var      string `toml:"var"`      // env:   variable name to read
	File     string `toml:"file"`     // file:  path to read (supports ~)
	Cmd      string `toml:"cmd"`      // cmd:   command whose stdout is the context
	Path     string `toml:"path"`     // alias: fixed subdirectory path
	Template string `toml:"template"` // path template; {value} is replaced with context value
}

// DefaultActions are used when no [[action]] blocks are declared in config.
var DefaultActions = []Action{
	{Name: "shell",   Builtin: "shell"},
	{Name: "editor",  Builtin: "editor"},
	{Name: "explore", Builtin: "explorer"},
	{Name: "print",   Builtin: "print"},
	{Name: "files",   Builtin: "files"},
	{Name: "run",     Builtin: "run"},
}

// BuiltinProfiles maps profile names to their action sets.
// Pass a profile name via `onix install -<profile>` to apply it.
var BuiltinProfiles = map[string][]Action{
	"sadirano": {
		{Name: "c", Builtin: "shell"},    // command line
		{Name: "f", Builtin: "files"},    // open file
		{Name: "n", Builtin: "editor"},   // neovim/editor
		{Name: "o", Builtin: "shell"},    // onix short alias
		{Name: "r", Builtin: "run"},      // run
		{Name: "s", Builtin: "explorer"}, // start/explorer
		{Name: "y", Builtin: "print"},    // yank/copy path
	},
}

// Dir returns the onix home directory (~/.onix).
// Panics when the home directory cannot be determined, as all subsequent
// path operations would silently resolve relative to CWD otherwise.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if h := os.Getenv("USERPROFILE"); h != "" {
			home = h
		} else if d, p := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"); d != "" && p != "" {
			home = d + p
		} else {
			panic("onix: cannot determine home directory (USERPROFILE and HOMEDRIVE/HOMEPATH are unset)")
		}
	}
	return filepath.Join(home, ".onix")
}

// Path returns the config file path (~/.onix/config.toml).
func Path() string {
	return filepath.Join(Dir(), "config.toml")
}

// ModulesDir returns the directory where module source + binaries live.
func ModulesDir() string {
	return filepath.Join(Dir(), "modules")
}

// BinDir returns the directory where .cmd wrappers live (add this to PATH).
func BinDir() string {
	return filepath.Join(Dir(), "bin")
}

// Settings holds global onix settings.
type Settings struct {
	AliasFile    string `toml:"alias_file"`    // override alias file; empty = use default
	Editor       string `toml:"editor"`       // override editor; empty = use EDITOR env
	Timing       bool   `toml:"timing"`       // equivalent to ONIX_TIMING=1
	Debug        bool   `toml:"debug"`        // equivalent to ONIX_DEBUG=1
	DisableRun   bool   `toml:"disable_run"`  // set true to block the run builtin
}

// Module describes one installable module.
type Module struct {
	Name    string         `toml:"name"`    // command name; inferred from repo if empty
	Repo    string         `toml:"repo"`    // "user/repo" on GitHub
	Ref     string         `toml:"ref"`     // branch, tag, or SHA; empty = default branch
	Enabled bool           `toml:"enabled"` // false = skip without removing
	Config  map[string]any `toml:"config"`  // passed as ONIX_MODULE_CONFIG JSON
	Entries []Entry        `toml:"entry"`   // optional multi-entry overrides
}

// EffectiveName returns the module name, falling back to the repo basename.
func (m *Module) EffectiveName() string {
	if m.Name != "" {
		return m.Name
	}
	parts := strings.Split(m.Repo, "/")
	return parts[len(parts)-1]
}

// ConfigJSON serialises the module's Config map to a JSON string suitable for
// the ONIX_MODULE_CONFIG environment variable.
func (m *Module) ConfigJSON() string {
	if len(m.Config) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m.Config)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Config is the top-level structure for ~/.onix/config.toml.
type Config struct {
	Settings Settings      `toml:"settings"`
	Context  ContextConfig `toml:"context"` // [context] table — optional
	Actions  []Action      `toml:"action"`  // [[action]] tables
	Modules  []Module      `toml:"module"`  // [[module]] tables
}

// Load reads and parses ~/.onix/config.toml.
// Returns an empty config if the file does not exist.
func Load() (*Config, error) {
	cfg := &Config{}
	// Apply defaults.
	cfg.Settings.Editor = ""

	p := Path()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return cfg, nil
	}

	type rawModule struct {
		Name    string         `toml:"name"`
		Repo    string         `toml:"repo"`
		Ref     string         `toml:"ref"`
		Enabled *bool          `toml:"enabled"`
		Config  map[string]any `toml:"config"`
		Entries []Entry        `toml:"entry"`
	}
	type rawConfig struct {
		Settings Settings      `toml:"settings"`
		Context  ContextConfig `toml:"context"`
		Actions  []Action      `toml:"action"`
		Modules  []rawModule   `toml:"module"`
	}

	var raw rawConfig
	if _, err := toml.DecodeFile(p, &raw); err != nil {
		return nil, err
	}

	cfg.Settings = raw.Settings
	cfg.Context = raw.Context
	cfg.Actions = raw.Actions
	cfg.Modules = make([]Module, 0, len(raw.Modules))
	for _, m := range raw.Modules {
		enabled := true
		if m.Enabled != nil {
			enabled = *m.Enabled
		}
		cfg.Modules = append(cfg.Modules, Module{
			Name:    m.Name,
			Repo:    m.Repo,
			Ref:     m.Ref,
			Enabled: enabled,
			Config:  m.Config,
			Entries: m.Entries,
		})
	}

	return cfg, nil
}

// Save writes cfg back to ~/.onix/config.toml.
func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(Path())
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

// HasContext reports whether a [context] section is present in config.toml.
// When false, sub-alias navigation builds paths without a context layer.
func (c *Config) HasContext() bool {
	return c.Context != (ContextConfig{})
}

// IsDebugEnabled reports whether debug output is active.
func (c *Config) IsDebugEnabled() bool {
	return c.Settings.Debug ||
		os.Getenv("ONIX_DEBUG") == "1"
}

// ResolveEditor returns the configured editor, falling back to EDITOR env then nvim.
func (c *Config) ResolveEditor() string {
	if e := strings.TrimSpace(c.Settings.Editor); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	return "nvim"
}

// FindAction returns the action with the given name, checking DefaultActions
// when no [[action]] blocks are declared. Returns nil when not found.
func (c *Config) FindAction(name string) *Action {
	actions := c.Actions
	if len(actions) == 0 {
		actions = DefaultActions
	}
	for i := range actions {
		if strings.EqualFold(actions[i].Name, name) {
			return &actions[i]
		}
	}
	return nil
}

// FindModule returns the module entry with the given name, or nil.
func (c *Config) FindModule(name string) *Module {
	for i := range c.Modules {
		if strings.EqualFold(c.Modules[i].EffectiveName(), name) {
			return &c.Modules[i]
		}
	}
	return nil
}

// NormalizeRepo strips URL prefixes and a trailing .git suffix so the result
// is always "user/repo".
func NormalizeRepo(repo string) string {
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	return repo
}

// ModuleDir returns the source/binary directory for a module repo under ModulesDir.
func ModuleDir(repo string) string {
	parts := strings.Split(NormalizeRepo(repo), "/")
	args := append([]string{ModulesDir()}, parts...)
	return filepath.Join(args...)
}

// RepoBinName returns the binary name for a repo (last path segment).
func RepoBinName(repo string) string {
	parts := strings.Split(NormalizeRepo(repo), "/")
	return parts[len(parts)-1]
}

// Starter returns the minimal config.toml content written by `onix init`.
const Starter = `# ~/.onix/config.toml
# Onix configuration.

[settings]
# alias_file    = ""     # default: ~/.onix/aliases
# editor        = ""     # default: $EDITOR env var, then nvim
# timing        = false
# debug         = false
# disable_run   = false  # set true to block the run builtin (shell execution)
# Context resolved at runtime for subalias@alias navigation.
# When this section is absent, subalias@alias paths omit the context layer.
#
# [context]
# source = "env"           # "env" | "file" | "cmd"
# var    = "current_sms"   # for source=env: env var to read
# file   = "~/.onix/ctx"   # for source=file: path to read (first line used)
# cmd    = "git rev-parse --abbrev-ref HEAD"  # for source=cmd

# Declare named command wrappers below. Run "onix shortcuts" to generate
# .cmd files in ~/.onix/bin/ that set ONIX_COMMAND when invoked.
# When no [[action]] blocks are declared the built-in defaults are used:
#   shell, editor, explore, print, files, run
#
# [[action]]
# name    = "editor"
# builtin = "editor"

# Declare modules below. onix will prompt to install them on first use.
# Run "onix add <user/repo>" to register a module, then "onix install" to build it.
#
# [[module]]
# name    = "mymodule"
# repo    = "user/repo"
# ref     = "main"
# enabled = true
#
# [module.config]
# key = "value"   # passed as ONIX_MODULE_CONFIG (JSON) — do NOT store secrets here;
#                 # env vars are visible to all child processes
`
