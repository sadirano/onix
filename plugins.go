package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// PluginsFile is the on-disk shape of ~/.onix/plugins.toml. We keep the
// outer wrapper around a [[plugins]] array so future top-level keys (e.g.
// a default mirror) can be added without breaking parsers.
type PluginsFile struct {
	Plugins []Plugin `toml:"plugins"`
}

// Plugin is one user-declared external plugin. `Name` is the wrapper
// command the user types (`tts`, `sg`, `timer`); `Repo` is the GitHub
// "user/repo" path; `SHA` pins the build to one commit (the safe path);
// `Unpinned` opts out of that pin and tracks the default branch.
//
// `Entries` is cached at install time from the plugin's own `onix.toml`.
// We snapshot it into our own config so snippet regeneration and doctor
// don't have to re-read the plugin source on every call.
type Plugin struct {
	Name     string         `toml:"name"`
	Repo     string         `toml:"repo"`
	SHA      string         `toml:"sha,omitempty"`
	Unpinned bool           `toml:"unpinned,omitempty"`
	Config   map[string]any `toml:"config,omitempty"`
	Entries  []PluginEntry  `toml:"entries,omitempty"`
}

// PluginEntry mirrors v1's `onix.toml` shape inside a plugin repo:
// each entry becomes a separate shell-function wrapper with its own
// ONIX_ENTRY value. `Cmd` overrides the wrapper command name when the
// plugin author wants a different surface (e.g. `t-start` instead of
// `start`); it defaults to `Name` when empty.
type PluginEntry struct {
	Name string `toml:"name"`
	Cmd  string `toml:"cmd,omitempty"`
}

// EffectiveCmd returns the wrapper command name for an entry, falling
// back to the entry's logical Name when no override was set.
func (e PluginEntry) EffectiveCmd() string {
	if e.Cmd != "" {
		return e.Cmd
	}
	return e.Name
}

// LoadPlugins reads ~/.onix/plugins.toml. A missing file is fine — it
// means "no plugins declared." Bad TOML is reported with the file path so
// the user can find what to fix.
func LoadPlugins(home string) (*PluginsFile, error) {
	p := pluginsConfigPath(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &PluginsFile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	pf := &PluginsFile{}
	if err := toml.Unmarshal(data, pf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return pf, nil
}

// SavePlugins writes the plugins file atomically (temp + rename, same
// pattern as the alias store) so a mid-write crash never leaves a
// half-formed plugins.toml that would lose every recorded SHA.
func SavePlugins(home string, pf *PluginsFile) error {
	p := pluginsConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}

	// Hand-roll the encoding so output is stable and reviewable. go-toml's
	// encoder is fine but the diff churn from map ordering would be noisy.
	var b strings.Builder
	b.WriteString("# onix plugins — edit with care, prefer 'onix plugin add' / 'onix plugin remove'\n\n")
	sorted := make([]Plugin, len(pf.Plugins))
	copy(sorted, pf.Plugins)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, pl := range sorted {
		writePluginBlock(&b, pl)
		b.WriteString("\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(p), ".plugins.*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

func writePluginBlock(b *strings.Builder, p Plugin) {
	b.WriteString("[[plugins]]\n")
	fmt.Fprintf(b, "name = %q\n", p.Name)
	fmt.Fprintf(b, "repo = %q\n", p.Repo)
	if p.SHA != "" {
		fmt.Fprintf(b, "sha  = %q\n", p.SHA)
	}
	if p.Unpinned {
		b.WriteString("unpinned = true\n")
	}
	if len(p.Config) > 0 {
		// JSON form is the most predictable representation across the
		// loose typing TOML allows. We don't try to round-trip every
		// shape exactly; the consuming plugin sees the values through
		// ONIX_MODULE_CONFIG JSON anyway.
		j, _ := json.Marshal(p.Config)
		fmt.Fprintf(b, "config = %s\n", tomlInlineFromJSON(j))
	}
	for _, e := range p.Entries {
		b.WriteString("[[plugins.entries]]\n")
		fmt.Fprintf(b, "name = %q\n", e.Name)
		if e.Cmd != "" {
			fmt.Fprintf(b, "cmd  = %q\n", e.Cmd)
		}
	}
}

// tomlInlineFromJSON converts a JSON byte slice into a TOML inline-table
// literal. We do this by hand because go-toml's encoder doesn't expose
// "encode a value as inline" cleanly. Inputs come from json.Marshal of
// the user's `config` map, so we're not parsing arbitrary JSON.
func tomlInlineFromJSON(j []byte) string {
	// Quick-and-dirty conversion: JSON object {"k": "v"} → TOML inline
	// {k = "v"}. We only handle the shapes go-toml produces for our
	// own values: object, array, string, number, bool. If the user
	// hand-edits config with exotic values they'll re-read fine but
	// our re-emit may go through go-toml on the next save.
	var v any
	if err := json.Unmarshal(j, &v); err != nil {
		return string(j)
	}
	return tomlInline(v)
}

func tomlInline(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s = %s", k, tomlInline(x[k])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, tomlInline(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return fmt.Sprintf("%q", x)
	case bool:
		return fmt.Sprintf("%t", x)
	case float64:
		// json.Unmarshal returns float64 for all JSON numbers. Detect
		// integer-valued floats so the output isn't littered with .0.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case nil:
		return `""`
	}
	return fmt.Sprintf("%q", fmt.Sprint(v))
}

// FindPlugin returns the plugin matching name (case-insensitive), or nil.
func (pf *PluginsFile) FindPlugin(name string) *Plugin {
	key := strings.ToLower(strings.TrimSpace(name))
	for i := range pf.Plugins {
		if strings.ToLower(pf.Plugins[i].Name) == key {
			return &pf.Plugins[i]
		}
	}
	return nil
}

// Remove deletes the plugin with the given name. Returns true when an
// entry was actually removed (vs no-op for an unknown name).
func (pf *PluginsFile) Remove(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	for i, p := range pf.Plugins {
		if strings.ToLower(p.Name) == key {
			pf.Plugins = append(pf.Plugins[:i], pf.Plugins[i+1:]...)
			return true
		}
	}
	return false
}

// validatePlugins enforces our collision and shape rules. Called by both
// `onix plugin add` (before write) and `LoadPlugins`-aware paths (so a
// hand-edited duplicate surfaces immediately, not at the next snippet
// regeneration). actions is passed in so collisions across the two
// systems are caught.
func validatePlugins(pf *PluginsFile, actions []Action) error {
	seen := map[string]string{} // wrapper name → "plugin:<name>" / "action:<name>" / "builtin"
	for _, a := range actions {
		seen[strings.ToLower(a.Name)] = "action:" + a.Name
	}
	for _, n := range builtinWrapperNames {
		seen[n] = "builtin:" + n
	}

	for i, p := range pf.Plugins {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("plugins[%d]: name is required", i)
		}
		if strings.TrimSpace(p.Repo) == "" {
			return fmt.Errorf("plugins[%d] %s: repo is required", i, p.Name)
		}
		if !validActionName(p.Name) {
			return fmt.Errorf("plugins[%d] %s: name must be [A-Za-z0-9_-]+", i, p.Name)
		}
		if !p.Unpinned && strings.TrimSpace(p.SHA) == "" {
			return fmt.Errorf("plugins[%d] %s: sha is required (or set unpinned = true)", i, p.Name)
		}

		// Plugin's primary wrapper name.
		key := strings.ToLower(p.Name)
		if owner, dup := seen[key]; dup {
			return fmt.Errorf("plugins[%d] %s: wrapper name conflicts with %s", i, p.Name, owner)
		}
		seen[key] = "plugin:" + p.Name

		// Entry wrappers — each carries its own command name.
		for j, e := range p.Entries {
			if strings.TrimSpace(e.Name) == "" {
				return fmt.Errorf("plugins[%d] %s: entries[%d] has empty name", i, p.Name, j)
			}
			cmd := e.EffectiveCmd()
			if !validActionName(cmd) {
				return fmt.Errorf("plugins[%d] %s: entry %q cmd %q must be [A-Za-z0-9_-]+", i, p.Name, e.Name, cmd)
			}
			eKey := strings.ToLower(cmd)
			if owner, dup := seen[eKey]; dup {
				return fmt.Errorf("plugins[%d] %s: entry %q wrapper %q conflicts with %s",
					i, p.Name, e.Name, cmd, owner)
			}
			seen[eKey] = "plugin:" + p.Name + "[" + e.Name + "]"
		}
	}
	return nil
}

// builtinWrapperNames is the list of names emitted by snippetBuiltins.
// Kept here (not in init.go) so the validator can refer to it without
// importing snippet generation as a circular dependency.
var builtinWrapperNames = []string{"o", "n", "s", "y", "r"}

// ConfigJSON serialises Plugin.Config for ONIX_MODULE_CONFIG. The empty
// map encodes as `{}` so plugins can always parse the value without a
// nil-check, matching v1's contract.
func (p *Plugin) ConfigJSON() string {
	if len(p.Config) == 0 {
		return "{}"
	}
	b, err := json.Marshal(p.Config)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// -----------------------------------------------------------------------------
// path / naming helpers
// -----------------------------------------------------------------------------

// pluginsConfigPath returns ~/.onix/plugins.toml.
func pluginsConfigPath(home string) string {
	return filepath.Join(home, "plugins.toml")
}

// pluginSourceDir returns the directory where we clone and build a plugin.
// For GitHub repos we mirror the user/repo layout under ~/.onix/plugins/.
// For local paths (used in development and smoke tests) we use a flat
// ~/.onix/plugins/local/<basename>/ layout — two local plugins with the
// same basename would conflict but that's rare and surfaces clearly.
func pluginSourceDir(home, repo string) string {
	if isLocalRepo(repo) {
		return filepath.Join(home, "plugins", "local", repoBasename(repo))
	}
	parts := strings.Split(normalizeRepo(repo), "/")
	return filepath.Join(append([]string{home, "plugins"}, parts...)...)
}

// pluginBinaryPath returns the full path of the built plugin binary. We
// always build with `-o <basename>.exe` so the binary name is predictable
// regardless of how `go build` would default it in the plugin's repo.
func pluginBinaryPath(home, repo string) string {
	return filepath.Join(pluginSourceDir(home, repo), pluginBinaryName(repo))
}

// pluginBinaryName is the base filename of the built binary: the repo's
// last segment + ".exe". `onix-tts.exe`, `onix-find.exe`, etc.
func pluginBinaryName(repo string) string {
	return repoBasename(repo) + ".exe"
}

// repoBasename returns the last meaningful segment of repo. Handles all
// three repo shapes: "user/repo" (GitHub), absolute Windows/Unix paths,
// and URL forms. Centralising the basename logic keeps every caller
// agreeing on what "onix-tts" means in the various layouts.
func repoBasename(repo string) string {
	if isLocalRepo(repo) {
		// filepath.Base handles both separator styles and trailing slashes.
		return filepath.Base(strings.TrimSuffix(strings.TrimSpace(repo), string(filepath.Separator)))
	}
	parts := strings.Split(normalizeRepo(repo), "/")
	return parts[len(parts)-1]
}

// isLocalRepo reports whether repo looks like a local filesystem path
// (or file:// URL) rather than a GitHub coordinate. Used by source-dir
// and binary-name helpers to switch on layout without each one re-
// implementing the heuristic.
func isLocalRepo(repo string) bool {
	r := strings.TrimSpace(repo)
	if strings.HasPrefix(r, "file://") {
		return true
	}
	// Drive letter: C:\… or C:/…
	if len(r) >= 3 && r[1] == ':' && (r[2] == '\\' || r[2] == '/') {
		return true
	}
	// Unix absolute or UNC.
	if strings.HasPrefix(r, "/") || strings.HasPrefix(r, `\\`) {
		return true
	}
	return false
}

// normalizeRepo strips URL prefixes and trailing ".git" so the result is
// always "user/repo". Local paths pass through unchanged — callers that
// care about local vs GitHub layout should branch on isLocalRepo first.
func normalizeRepo(repo string) string {
	if isLocalRepo(repo) {
		return strings.TrimSpace(repo)
	}
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	return repo
}

// defaultWrapperName derives the user-facing wrapper command from the
// repo basename. Strips a leading "onix-" so "sadirano/onix-tts" becomes
// "tts" by default; users can override via `name = "..."` in plugins.toml.
func defaultWrapperName(repo string) string {
	return strings.TrimPrefix(repoBasename(repo), "onix-")
}
