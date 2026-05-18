package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/sadirano/onix/internal/config"
)

// PluginsFile is the on-disk shape of ~/.onix/plugins.toml.
type PluginsFile struct {
	Plugins []Plugin `toml:"plugins"`
}

// Plugin is one user-declared external plugin.
type Plugin struct {
	Name     string         `toml:"name"`
	Repo     string         `toml:"repo"`
	SHA      string         `toml:"sha,omitempty"`
	Unpinned bool           `toml:"unpinned,omitempty"`
	Config   map[string]any `toml:"config,omitempty"`
	Entries  []PluginEntry  `toml:"entries,omitempty"`
}

// PluginEntry is one entry block from a plugin repo's onix.toml.
type PluginEntry struct {
	Name string `toml:"name"`
	Cmd  string `toml:"cmd,omitempty"`
}

// EffectiveCmd returns the wrapper command name for an entry.
func (e PluginEntry) EffectiveCmd() string {
	if e.Cmd != "" {
		return e.Cmd
	}
	return e.Name
}

// ConfigPath returns home/plugins.toml.
func ConfigPath(home string) string {
	return filepath.Join(home, "plugins.toml")
}

// LoadPlugins reads ~/.onix/plugins.toml.
func LoadPlugins(home string) (*PluginsFile, error) {
	p := ConfigPath(home)
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

// SavePlugins writes the plugins file atomically.
func SavePlugins(home string, pf *PluginsFile) error {
	p := ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}

	// Stable sort for consistent output.
	sort.Slice(pf.Plugins, func(i, j int) bool { return pf.Plugins[i].Name < pf.Plugins[j].Name })

	data, err := toml.Marshal(pf)
	if err != nil {
		return fmt.Errorf("marshal plugins: %w", err)
	}

	var b strings.Builder
	b.WriteString("# onix plugins — edit with care, prefer 'onix plugin add' / 'onix plugin remove'\n\n")
	b.Write(data)

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

// Remove deletes the plugin with the given name.
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

// ValidatePlugins enforces our collision and shape rules.
func ValidatePlugins(pf *PluginsFile, actions []config.Action) error {
	seen := map[string]string{}
	for _, a := range actions {
		seen[strings.ToLower(a.Name)] = "action:" + a.Name
	}
	for _, n := range BuiltinWrapperNames {
		seen[n] = "builtin:" + n
	}

	for i, p := range pf.Plugins {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("plugins[%d]: name is required", i)
		}
		if strings.TrimSpace(p.Repo) == "" {
			return fmt.Errorf("plugins[%d] %s: repo is required", i, p.Name)
		}
		if !config.ValidActionName(p.Name) {
			return fmt.Errorf("plugins[%d] %s: name must be [A-Za-z0-9_-]+", i, p.Name)
		}
		if !p.Unpinned && strings.TrimSpace(p.SHA) == "" {
			return fmt.Errorf("plugins[%d] %s: sha is required (or set unpinned = true)", i, p.Name)
		}

		key := strings.ToLower(p.Name)
		if owner, dup := seen[key]; dup {
			return fmt.Errorf("plugins[%d] %s: wrapper name conflicts with %s", i, p.Name, owner)
		}
		seen[key] = "plugin:" + p.Name

		for j, e := range p.Entries {
			if strings.TrimSpace(e.Name) == "" {
				return fmt.Errorf("plugins[%d] %s: entries[%d] has empty name", i, p.Name, j)
			}
			cmd := e.EffectiveCmd()
			if !config.ValidActionName(cmd) {
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

// BuiltinWrapperNames is the list of names emitted by snippetBuiltins.
var BuiltinWrapperNames = []string{"o", "n", "s", "y", "r", "sg", "ff"}

// ConfigJSON serialises Plugin.Config for ONIX_MODULE_CONFIG.
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

// SourceDir returns the directory where we clone and build a plugin.
func SourceDir(home, repo string) string {
	if IsLocalRepo(repo) {
		return filepath.Join(home, "plugins", "local", RepoBasename(repo))
	}
	parts := strings.Split(NormalizeRepo(repo), "/")
	return filepath.Join(append([]string{home, "plugins"}, parts...)...)
}

// BinaryPath returns the full path of the built plugin binary.
func BinaryPath(home, repo string) string {
	return filepath.Join(SourceDir(home, repo), BinaryName(repo))
}

// BinaryName is the base filename of the built binary.
func BinaryName(repo string) string {
	name := RepoBasename(repo)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// RepoBasename returns the last meaningful segment of repo.
func RepoBasename(repo string) string {
	if IsLocalRepo(repo) {
		return filepath.Base(strings.TrimSuffix(strings.TrimSpace(repo), string(filepath.Separator)))
	}
	parts := strings.Split(NormalizeRepo(repo), "/")
	return parts[len(parts)-1]
}

// IsLocalRepo reports whether repo looks like a local filesystem path.
func IsLocalRepo(repo string) bool {
	r := strings.TrimSpace(repo)
	if strings.HasPrefix(r, "file://") {
		return true
	}
	if len(r) >= 3 && r[1] == ':' && (r[2] == '\\' || r[2] == '/') {
		return true
	}
	if strings.HasPrefix(r, "/") || strings.HasPrefix(r, `\\`) {
		return true
	}
	return false
}

// NormalizeRepo strips URL prefixes and trailing ".git".
func NormalizeRepo(repo string) string {
	if IsLocalRepo(repo) {
		return strings.TrimSpace(repo)
	}
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	return repo
}

// DefaultWrapperName derives the user-facing wrapper command from the repo basename.
func DefaultWrapperName(repo string) string {
	return strings.TrimPrefix(RepoBasename(repo), "onix-")
}
