package alias

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/sadirano/onix/internal/config"
)

const EnvVar = "ONIX_ALIAS_DIR"

// Entry is the full definition of one alias stored as a Lua file.
type Entry struct {
	Path     string                          // resolved target directory
	Context  config.ContextConfig            // optional alias-level context
	Segments map[string]config.ContextConfig // optional per-segment contexts, keyed by segment name
}

// Dir returns the aliases directory path.
// Precedence: ONIX_ALIAS_DIR > settings.alias_dir > ~/.onix/aliases
func Dir() string {
	if v := strings.TrimSpace(os.Getenv(EnvVar)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".onix", "aliases")
	}
	return filepath.Join(home, ".onix", "aliases")
}

// ---------------------------------------------------------------------------
// Shared Lua VM + in-process entry cache
//
// A single Lua state is created once per process. Alias files are pure data,
// so opening stdlib is optional — but we include it so users can use os.getenv
// and string functions in their alias files if they want to.
//
// The entry cache means that for segmented navigation (sg@task@acme), the
// alias file is parsed once on first access and all subsequent lookups for the
// same alias within the same process are a 22ns map read — faster than the old
// approach which required separate file reads for each segment context.
// ---------------------------------------------------------------------------

var (
	vmOnce  sync.Once
	vm      *lua.LState
	entries sync.Map // string → *Entry
)

func sharedVM() *lua.LState {
	vmOnce.Do(func() {
		vm = lua.NewState()
	})
	return vm
}

// InvalidateCache removes the cached entry for name so the next Load call
// re-reads the file. Called by Save after writing a new version to disk.
func InvalidateCache(name string) {
	entries.Delete(name)
}

// Load reads and parses ~/.onix/aliases/<name>.lua.
// Results are cached for the lifetime of the process; call InvalidateCache
// after writing a new version to disk.
// Returns (nil, nil) when the file does not exist.
func Load(name string) (*Entry, error) {
	if v, ok := entries.Load(name); ok {
		return v.(*Entry), nil
	}

	p := filepath.Join(Dir(), name+".lua")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}

	L := sharedVM()
	if err := L.DoFile(p); err != nil {
		L.SetTop(0)
		return nil, fmt.Errorf("%s.lua: %w", name, err)
	}

	tbl, ok := L.Get(-1).(*lua.LTable)
	if !ok {
		L.SetTop(0)
		return nil, fmt.Errorf("%s.lua must return a table", name)
	}

	e := &Entry{
		Path: luaStr(tbl, "path"),
	}

	if ct, ok := tbl.RawGetString("context").(*lua.LTable); ok {
		e.Context = parseContextConfig(ct)
	}

	if st, ok := tbl.RawGetString("segments").(*lua.LTable); ok {
		e.Segments = make(map[string]config.ContextConfig)
		st.ForEach(func(k, v lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				if vt, ok := v.(*lua.LTable); ok {
					e.Segments[string(ks)] = parseContextConfig(vt)
				}
			}
		})
	}

	L.SetTop(0) // clear stack — return value consumed, VM reused next call
	entries.Store(name, e)
	return e, nil
}

// Save writes e to ~/.onix/aliases/<name>.lua and invalidates the cache entry.
func Save(name string, e *Entry) error {
	d := Dir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("return {\n")
	fmt.Fprintf(&b, "  path = %q,\n", e.Path)

	if e.Context != (config.ContextConfig{}) {
		b.WriteString("  context = ")
		writeContextLua(&b, e.Context, "  ")
		b.WriteString(",\n")
	}

	if len(e.Segments) > 0 {
		b.WriteString("  segments = {\n")
		for seg, cc := range e.Segments {
			fmt.Fprintf(&b, "    [%q] = ", seg)
			writeContextLua(&b, cc, "    ")
			b.WriteString(",\n")
		}
		b.WriteString("  },\n")
	}

	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(d, name+".lua"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	InvalidateCache(name)
	return nil
}

// Register creates or updates the alias file for name, setting its path.
// Existing context and segment data are preserved.
func Register(name, destination string) error {
	e, err := Load(name)
	if err != nil {
		return err
	}
	if e == nil {
		e = &Entry{}
	}
	e.Path = destination
	return Save(name, e)
}

// Resolve returns the target directory for the given alias name.
// Falls back to a raw filesystem path when no alias file exists.
func Resolve(input string, debug bool) (string, error) {
	e, err := Load(input)
	if err != nil {
		return "", err
	}
	if e != nil && e.Path != "" {
		if debug {
			fmt.Printf("[ONIX] resolved %q -> %q\n", input, e.Path)
		}
		return e.Path, nil
	}

	// Fall back to a raw path if it exists on disk.
	if _, serr := os.Stat(input); serr == nil {
		abs, err := filepath.Abs(input)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", input, err)
		}
		return abs, nil
	}

	return "", fmt.Errorf("unknown alias %q — create ~/.onix/aliases/%s.lua with path = \"...\"", input, input)
}

// OpenInEditor opens the aliases directory in the given editor.
func OpenInEditor(editor string) error {
	d := Dir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	cmd := exec.Command(editor, d)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open editor: %w", err)
	}
	return cmd.Wait()
}

// ApplyEnvOverride propagates aliasDir into ONIX_ALIAS_DIR so that child
// processes inherit the same aliases directory as the parent.
func ApplyEnvOverride(aliasDir string) {
	if strings.TrimSpace(aliasDir) == "" {
		return
	}
	if strings.TrimSpace(os.Getenv(EnvVar)) != "" {
		return
	}
	if err := os.Setenv(EnvVar, aliasDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not set %s: %v\n", EnvVar, err)
	}
}

// ---------------------------------------------------------------------------
// Lua helpers
// ---------------------------------------------------------------------------

func luaStr(t *lua.LTable, key string) string {
	if s, ok := t.RawGetString(key).(lua.LString); ok {
		return string(s)
	}
	return ""
}

func parseContextConfig(t *lua.LTable) config.ContextConfig {
	return config.ContextConfig{
		Source:   luaStr(t, "source"),
		Var:      luaStr(t, "var"),
		File:     luaStr(t, "file"),
		Cmd:      luaStr(t, "cmd"),
		Path:     luaStr(t, "path"),
		Template: luaStr(t, "template"),
	}
}

func writeContextLua(b *strings.Builder, cc config.ContextConfig, indent string) {
	b.WriteString("{\n")
	if cc.Source != "" {
		fmt.Fprintf(b, "%s  source   = %q,\n", indent, cc.Source)
	}
	if cc.Var != "" {
		fmt.Fprintf(b, "%s  var      = %q,\n", indent, cc.Var)
	}
	if cc.File != "" {
		fmt.Fprintf(b, "%s  file     = %q,\n", indent, cc.File)
	}
	if cc.Cmd != "" {
		fmt.Fprintf(b, "%s  cmd      = %q,\n", indent, cc.Cmd)
	}
	if cc.Path != "" {
		fmt.Fprintf(b, "%s  path     = %q,\n", indent, cc.Path)
	}
	if cc.Template != "" {
		fmt.Fprintf(b, "%s  template = %q,\n", indent, cc.Template)
	}
	fmt.Fprintf(b, "%s}", indent)
}
