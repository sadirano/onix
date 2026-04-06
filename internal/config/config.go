package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Dir returns the onix home directory (~/.onix).
func Dir() string {
	home, _ := os.UserHomeDir()
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
	AliasFile string `toml:"alias_file"` // override alias file; empty = use default
	Editor    string `toml:"editor"`     // override editor; empty = use EDITOR env
	Timing    bool   `toml:"timing"`     // equivalent to ONIX_TIMING=1
	Debug     bool   `toml:"debug"`      // equivalent to ONIX_DEBUG=1
}

// Module describes one installable module.
type Module struct {
	Name    string         `toml:"name"`    // command name; inferred from repo if empty
	Repo    string         `toml:"repo"`    // "user/repo" on GitHub
	Ref     string         `toml:"ref"`     // branch, tag, or SHA; empty = default branch
	Enabled bool           `toml:"enabled"` // false = skip without removing
	Config  map[string]any `toml:"config"`  // passed as ONIX_MODULE_CONFIG JSON
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
	b, _ := json.Marshal(m.Config)
	return string(b)
}

// Config is the top-level structure for ~/.onix/config.toml.
type Config struct {
	Settings Settings `toml:"settings"`
	Modules  []Module `toml:"module"` // [[module]] tables
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
	}
	type rawConfig struct {
		Settings Settings    `toml:"settings"`
		Modules  []rawModule `toml:"module"`
	}

	var raw rawConfig
	if _, err := toml.DecodeFile(p, &raw); err != nil {
		return nil, err
	}

	cfg.Settings = raw.Settings
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

// FindModule returns the module entry with the given name, or nil.
func (c *Config) FindModule(name string) *Module {
	for i := range c.Modules {
		if strings.EqualFold(c.Modules[i].EffectiveName(), name) {
			return &c.Modules[i]
		}
	}
	return nil
}

// Starter returns the minimal config.toml content written by `onix init`.
const Starter = `# ~/.onix/config.toml
# Onix module manager configuration.
# Inspired by lazy.nvim: declare modules here, then run "onix install" to
# download, build, and wire up the .cmd wrappers in ~/.onix/bin/.

[settings]
# alias_file = ""   # default: ~/.omni/.env  (omni-compatible)
# editor     = ""   # default: $EDITOR env var, then nvim
# timing     = false
# debug      = false

# Declare modules below. Example:
#
# [[module]]
# name    = "sg"
# repo    = "sadirano/onix-sg"
# ref     = "main"
# enabled = true
#
# [module.config]
# default_flags = "--type go"
`
