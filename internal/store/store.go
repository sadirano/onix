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
type Alias struct {
	Path        string   `toml:"path"`
	Description string   `toml:"description,omitempty"`
	Tags        []string `toml:"tags,omitempty"`
	Owner       string   `toml:"owner,omitempty"`
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

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}

	s := &Store{Aliases: map[string]Alias{}}
	for name, v := range raw {
		// Round-trip through marshal to unmarshal into the Alias struct.
		// This is robust to schema changes and avoids manual map-to-struct mapping.
		b, _ := toml.Marshal(v)
		var a Alias
		if err := toml.Unmarshal(b, &a); err != nil {
			return nil, fmt.Errorf("parse %s alias %q: %w", p, name, err)
		}
		if err := ValidateAliasName(name); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		s.Aliases[name] = a
	}

	if needs, lowered := lowerKeys(s.Aliases); needs {
		s.Aliases = lowered
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

func lowerKeys(in map[string]Alias) (bool, map[string]Alias) {
	dirty := false
	for k := range in {
		if k != strings.ToLower(k) {
			dirty = true
			break
		}
	}
	if !dirty {
		return false, nil
	}
	out := make(map[string]Alias, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return true, out
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

// ExpandTilde expands a leading ~ to the user home directory.
func ExpandTilde(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return home + p[1:]
}
