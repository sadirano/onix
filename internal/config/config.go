package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Entry describes one entry point exposed by a module binary.
// When a module has multiple entry points, each gets its own .cmd wrapper.
type Entry struct {
	Name string // sub-command passed to the module binary
	Cmd  string // .cmd wrapper filename; defaults to Name
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
	Name    string // wrapper name (e.g. "editor") and ONIX_COMMAND value
	Builtin string // which built-in behaviour to run: shell|editor|explorer|print|files|run
}

// ContextConfig defines how to resolve the active working context at runtime.
// When absent from config.lua the zero value is used and HasContext() returns
// false — sub-alias navigation then works without a context layer.
type ContextConfig struct {
	Source   string // "env" (default) | "file" | "cmd" | "alias"
	Var      string // env:   variable name to read
	File     string // file:  path to read (supports ~)
	Cmd      string // cmd:   command whose stdout is the context
	Path     string // alias: fixed subdirectory path
	Template string // path template; {value} is replaced with context value
}

// DefaultActions are used when no actions are declared in config.
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

// Path returns the config file path (~/.onix/config.lua).
func Path() string {
	return filepath.Join(Dir(), "config.lua")
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
	AliasFile  string // override alias file; empty = use default
	Editor     string // override editor; empty = use EDITOR env
	Timing     bool   // equivalent to ONIX_TIMING=1
	Debug      bool   // equivalent to ONIX_DEBUG=1
	DisableRun bool   // set true to block the run builtin
}

// Module describes one installable module.
type Module struct {
	Name    string         // command name; inferred from repo if empty
	Repo    string         // "user/repo" on GitHub
	Ref     string         // branch, tag, or SHA; empty = default branch
	Enabled bool           // false = skip without removing
	Config  map[string]any // passed as ONIX_MODULE_CONFIG JSON
	Entries []Entry        // optional multi-entry overrides
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

// Config is the top-level structure for ~/.onix/config.lua.
type Config struct {
	Settings Settings
	Context  ContextConfig // context table — optional
	Actions  []Action      // actions array
	Modules  []Module      // modules array
}

// Load reads and parses ~/.onix/config.lua.
// Returns an empty config if the file does not exist.
func Load() (*Config, error) {
	cfg := &Config{}

	p := Path()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		// Helpful hint when a legacy config.toml exists.
		if _, terr := os.Stat(filepath.Join(Dir(), "config.toml")); terr == nil {
			fmt.Fprintf(os.Stderr, "onix: found config.toml — rename it to config.lua and convert to Lua table syntax\n")
		}
		return cfg, nil
	}

	L := lua.NewState()
	defer L.Close()

	if err := L.DoFile(p); err != nil {
		return nil, fmt.Errorf("config.lua: %w", err)
	}

	tbl, ok := L.Get(-1).(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("config.lua must return a table (got %T)", L.Get(-1))
	}

	if st, ok := tbl.RawGetString("settings").(*lua.LTable); ok {
		cfg.Settings = Settings{
			AliasFile:  luaStr(st, "alias_file"),
			Editor:     luaStr(st, "editor"),
			Timing:     luaBool(st, "timing"),
			Debug:      luaBool(st, "debug"),
			DisableRun: luaBool(st, "disable_run"),
		}
	}

	if ct, ok := tbl.RawGetString("context").(*lua.LTable); ok {
		cfg.Context = ContextConfig{
			Source:   luaStr(ct, "source"),
			Var:      luaStr(ct, "var"),
			File:     luaStr(ct, "file"),
			Cmd:      luaStr(ct, "cmd"),
			Path:     luaStr(ct, "path"),
			Template: luaStr(ct, "template"),
		}
	}

	if at, ok := tbl.RawGetString("actions").(*lua.LTable); ok {
		n := at.MaxN()
		for i := 1; i <= n; i++ {
			if t, ok := at.RawGetInt(i).(*lua.LTable); ok {
				cfg.Actions = append(cfg.Actions, Action{
					Name:    luaStr(t, "name"),
					Builtin: luaStr(t, "builtin"),
				})
			}
		}
	}

	if mt, ok := tbl.RawGetString("modules").(*lua.LTable); ok {
		n := mt.MaxN()
		for i := 1; i <= n; i++ {
			t, ok := mt.RawGetInt(i).(*lua.LTable)
			if !ok {
				continue
			}
			enabled := true
			if ev := t.RawGetString("enabled"); ev != lua.LNil {
				if b, ok := ev.(lua.LBool); ok {
					enabled = bool(b)
				}
			}
			mod := Module{
				Name:    luaStr(t, "name"),
				Repo:    luaStr(t, "repo"),
				Ref:     luaStr(t, "ref"),
				Enabled: enabled,
			}
			if ct, ok := t.RawGetString("config").(*lua.LTable); ok {
				mod.Config = luaTableToMap(ct)
			}
			if et, ok := t.RawGetString("entries").(*lua.LTable); ok {
				en := et.MaxN()
				for j := 1; j <= en; j++ {
					if e, ok := et.RawGetInt(j).(*lua.LTable); ok {
						mod.Entries = append(mod.Entries, Entry{
							Name: luaStr(e, "name"),
							Cmd:  luaStr(e, "cmd"),
						})
					}
				}
			}
			cfg.Modules = append(cfg.Modules, mod)
		}
	}

	return cfg, nil
}

// Save writes cfg back to ~/.onix/config.lua.
func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("return {\n")

	b.WriteString("  settings = {\n")
	if cfg.Settings.AliasFile != "" {
		fmt.Fprintf(&b, "    alias_file  = %q,\n", cfg.Settings.AliasFile)
	}
	if cfg.Settings.Editor != "" {
		fmt.Fprintf(&b, "    editor      = %q,\n", cfg.Settings.Editor)
	}
	if cfg.Settings.Timing {
		b.WriteString("    timing      = true,\n")
	}
	if cfg.Settings.Debug {
		b.WriteString("    debug       = true,\n")
	}
	if cfg.Settings.DisableRun {
		b.WriteString("    disable_run = true,\n")
	}
	b.WriteString("  },\n\n")

	if cfg.HasContext() {
		b.WriteString("  context = {\n")
		if cfg.Context.Source != "" {
			fmt.Fprintf(&b, "    source   = %q,\n", cfg.Context.Source)
		}
		if cfg.Context.Var != "" {
			fmt.Fprintf(&b, "    var      = %q,\n", cfg.Context.Var)
		}
		if cfg.Context.File != "" {
			fmt.Fprintf(&b, "    file     = %q,\n", cfg.Context.File)
		}
		if cfg.Context.Cmd != "" {
			fmt.Fprintf(&b, "    cmd      = %q,\n", cfg.Context.Cmd)
		}
		if cfg.Context.Path != "" {
			fmt.Fprintf(&b, "    path     = %q,\n", cfg.Context.Path)
		}
		if cfg.Context.Template != "" {
			fmt.Fprintf(&b, "    template = %q,\n", cfg.Context.Template)
		}
		b.WriteString("  },\n\n")
	}

	if len(cfg.Actions) > 0 {
		b.WriteString("  actions = {\n")
		for _, a := range cfg.Actions {
			fmt.Fprintf(&b, "    { name = %q, builtin = %q },\n", a.Name, a.Builtin)
		}
		b.WriteString("  },\n\n")
	}

	if len(cfg.Modules) > 0 {
		b.WriteString("  modules = {\n")
		for _, m := range cfg.Modules {
			b.WriteString("    {\n")
			if m.Name != "" {
				fmt.Fprintf(&b, "      name    = %q,\n", m.Name)
			}
			fmt.Fprintf(&b, "      repo    = %q,\n", m.Repo)
			if m.Ref != "" {
				fmt.Fprintf(&b, "      ref     = %q,\n", m.Ref)
			}
			if !m.Enabled {
				b.WriteString("      enabled = false,\n")
			}
			if len(m.Config) > 0 {
				b.WriteString("      config  = ")
				writeMapLua(&b, m.Config, "      ")
				b.WriteString(",\n")
			}
			if len(m.Entries) > 0 {
				b.WriteString("      entries = {\n")
				for _, e := range m.Entries {
					fmt.Fprintf(&b, "        { name = %q, cmd = %q },\n", e.Name, e.Cmd)
				}
				b.WriteString("      },\n")
			}
			b.WriteString("    },\n")
		}
		b.WriteString("  },\n")
	}

	b.WriteString("}\n")
	return os.WriteFile(Path(), []byte(b.String()), 0o644)
}

// HasContext reports whether a context section is present in config.lua.
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
// when no actions are declared. Returns nil when not found.
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

// Starter is the minimal config.lua content written by `onix init`.
const Starter = `-- ~/.onix/config.lua
-- Onix configuration.

return {
  settings = {
    -- alias_file  = "",       -- default: ~/.onix/aliases
    -- editor      = "",       -- default: $EDITOR env var, then nvim
    -- timing      = false,
    -- debug       = false,
    -- disable_run = false,    -- set true to block the run builtin (shell execution)
  },

  -- Context resolved at runtime for subalias@alias navigation.
  -- When this section is absent, subalias@alias paths omit the context layer.
  --
  -- context = {
  --   source = "env",           -- "env" | "file" | "cmd"
  --   var    = "current_sms",   -- for source=env: env var to read
  --   file   = "~/.onix/ctx",   -- for source=file: path to read (first line used)
  --   cmd    = "git rev-parse --abbrev-ref HEAD",  -- for source=cmd
  -- },

  -- Declare named command wrappers below. Run "onix shortcuts" to generate
  -- .cmd files in ~/.onix/bin/ that set ONIX_COMMAND when invoked.
  -- When no actions are declared the built-in defaults are used:
  --   shell, editor, explore, print, files, run
  --
  -- actions = {
  --   { name = "editor", builtin = "editor" },
  -- },

  -- Declare modules below. onix will prompt to install them on first use.
  -- Run "onix add <user/repo>" to register a module, then "onix install" to build it.
  --
  -- modules = {
  --   {
  --     name    = "mymodule",
  --     repo    = "user/repo",
  --     ref     = "main",
  --     enabled = true,
  --     config  = { key = "value" },  -- passed as ONIX_MODULE_CONFIG (JSON)
  --                                   -- do NOT store secrets here;
  --                                   -- env vars are visible to all child processes
  --   },
  -- },
}
`

// ---------------------------------------------------------------------------
// Lua helpers
// ---------------------------------------------------------------------------

func luaStr(t *lua.LTable, key string) string {
	if s, ok := t.RawGetString(key).(lua.LString); ok {
		return string(s)
	}
	return ""
}

func luaBool(t *lua.LTable, key string) bool {
	if b, ok := t.RawGetString(key).(lua.LBool); ok {
		return bool(b)
	}
	return false
}

func luaTableToMap(t *lua.LTable) map[string]any {
	m := make(map[string]any)
	t.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			m[string(ks)] = luaToAny(v)
		}
	})
	return m
}

func luaToAny(v lua.LValue) any {
	switch v := v.(type) {
	case lua.LString:
		return string(v)
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		f := float64(v)
		if f == float64(int64(f)) {
			return int64(f)
		}
		return f
	case *lua.LTable:
		if v.MaxN() > 0 {
			arr := make([]any, v.MaxN())
			for i := 1; i <= v.MaxN(); i++ {
				arr[i-1] = luaToAny(v.RawGetInt(i))
			}
			return arr
		}
		return luaTableToMap(v)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Lua serialisation helpers (used by Save)
// ---------------------------------------------------------------------------

func writeMapLua(b *strings.Builder, m map[string]any, indent string) {
	b.WriteString("{\n")
	for k, v := range m {
		fmt.Fprintf(b, "%s  %s = ", indent, k)
		writeValueLua(b, v, indent+"  ")
		b.WriteString(",\n")
	}
	fmt.Fprintf(b, "%s}", indent)
}

func writeValueLua(b *strings.Builder, v any, indent string) {
	switch v := v.(type) {
	case string:
		fmt.Fprintf(b, "%q", v)
	case bool:
		fmt.Fprintf(b, "%t", v)
	case int64:
		fmt.Fprintf(b, "%d", v)
	case float64:
		if v == float64(int64(v)) {
			fmt.Fprintf(b, "%d", int64(v))
		} else {
			fmt.Fprintf(b, "%g", v)
		}
	case map[string]any:
		writeMapLua(b, v, indent)
	case []any:
		b.WriteString("{\n")
		for _, item := range v {
			fmt.Fprintf(b, "%s  ", indent)
			writeValueLua(b, item, indent+"  ")
			b.WriteString(",\n")
		}
		fmt.Fprintf(b, "%s}", indent)
	default:
		fmt.Fprintf(b, "%q", fmt.Sprint(v))
	}
}
