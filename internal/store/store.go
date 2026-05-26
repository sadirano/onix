package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Store is the on-disk alias database. One TOML file, all aliases.
type Store struct {
	Aliases map[string]Alias
}

// Alias is one alias entry.
//
// Single-target aliases use Path. Multi-target aliases use Paths; when Paths
// is non-empty it takes precedence over Path. Resolution of multiple targets
// is handled by the caller (typically an interactive fzf picker).
type Alias struct {
	Path        string   `toml:"path,omitempty"`
	Paths       []string `toml:"paths,omitempty"`
	Description string   `toml:"description,omitempty"`
	Tags        []string `toml:"tags,omitempty"`
	Owner       string   `toml:"owner,omitempty"`
}

// AllPaths returns the effective set of paths for this alias.
// Paths takes precedence over Path for multi-target aliases.
func (a Alias) AllPaths() []string {
	if len(a.Paths) > 0 {
		return a.Paths
	}
	if a.Path != "" {
		return []string{a.Path}
	}
	return nil
}

// AliasesPath returns home/aliases.toml.
func AliasesPath(home string) string {
	return filepath.Join(home, "aliases.toml")
}

// LoadStore reads the aliases.toml at home/aliases.toml.
func LoadStore(home string) (*Store, error) {
	p := AliasesPath(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Aliases: map[string]Alias{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}

	s := &Store{Aliases: map[string]Alias{}}
	if err := toml.Unmarshal(data, &s.Aliases); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	for name := range s.Aliases {
		if err := ValidateAliasName(name); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if name != strings.ToLower(name) {
			return nil, fmt.Errorf("%s: alias key %q must be lowercase", p, name)
		}
	}
	return s, nil
}

// Lookup returns the alias by name (case-insensitive).
func (s *Store) Lookup(name string) (Alias, bool) {
	a, ok := s.Aliases[strings.ToLower(strings.TrimSpace(name))]
	return a, ok
}

// Set adds or replaces an alias.
func (s *Store) Set(name string, a Alias) {
	if s.Aliases == nil {
		s.Aliases = map[string]Alias{}
	}
	s.Aliases[strings.ToLower(strings.TrimSpace(name))] = a
}

// Delete removes an alias.
func (s *Store) Delete(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if _, ok := s.Aliases[key]; !ok {
		return false
	}
	delete(s.Aliases, key)
	return true
}

// Names returns the alias names in sorted order.
func (s *Store) Names() []string {
	out := make([]string, 0, len(s.Aliases))
	for k := range s.Aliases {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SaveStore writes the store to home/aliases.toml atomically.
func SaveStore(home string, s *Store) error {
	p := AliasesPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}

	out := make(map[string]any, len(s.Aliases))
	for k, v := range s.Aliases {
		out[k] = v
	}

	data, err := toml.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}

	var b strings.Builder
	b.WriteString("# onix aliases — edit with care, prefer `onix <name> <path>` / `onix <name> --remove`\n\n")
	b.Write(data)

	tmp, err := os.CreateTemp(filepath.Dir(p), ".aliases.*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// ValidateAliasName returns an error if name is not a legal alias name.
func ValidateAliasName(name string) error {
	return validateName("alias", name)
}

// ValidateSegmentName returns an error if name is not a legal segment name.
func ValidateSegmentName(name string) error {
	return validateName("segment", name)
}

func validateName(kind, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s name cannot be empty", kind)
	}
	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			return fmt.Errorf("%s name cannot contain path separators ('/' or '\\'): %q", kind, name)
		case r == '@':
			return fmt.Errorf("%s name cannot contain '@' (the segment separator): %q", kind, name)
		case r <= ' ' || r == 0x7f:
			return fmt.Errorf("%s name cannot contain whitespace or control characters: %q", kind, name)
		}
	}
	return nil
}

// ExpandTilde expands a leading ~/ (or a bare ~) to the user home directory.
// It intentionally does NOT expand ~user/... forms: those refer to other users'
// home directories and the resolution would be OS-dependent and wrong on Windows.
func ExpandTilde(p string) string {
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return home
	}
	if !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return home + p[1:]
}
