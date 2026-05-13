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
	Version int              `toml:"version"`
	Aliases map[string]Alias `toml:"-"`
}

// CurrentVersion is the latest schema version for aliases.toml.
const CurrentVersion = 2

// Alias is one alias entry.
type Alias struct {
	Path    string            `toml:"path"`
	Subdirs map[string]string `toml:"subdirs,omitempty"`
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
		return &Store{Version: CurrentVersion, Aliases: map[string]Alias{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}

	// We decode into a raw map to see what's there.
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}

	s := &Store{Aliases: map[string]Alias{}}
	if v, ok := doc["version"].(int64); ok {
		s.Version = int(v)
	}
	delete(doc, "version")

	// Now re-marshal the rest and unmarshal into the map.
	rest, _ := toml.Marshal(doc)
	if err := toml.Unmarshal(rest, &s.Aliases); err != nil {
		return nil, fmt.Errorf("parse %s aliases: %w", p, err)
	}

	for name, a := range s.Aliases {
		if err := ValidateAliasName(name); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for seg := range a.Subdirs {
			if err := ValidateSegmentName(seg); err != nil {
				return nil, fmt.Errorf("%s: alias %q: %w", p, name, err)
			}
		}
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

	var b strings.Builder
	b.WriteString("# onix aliases — edit with care, prefer `onix add` / `onix rm`\n\n")
	fmt.Fprintf(&b, "version = %d\n\n", CurrentVersion)
	for _, name := range s.Names() {
		a := s.Aliases[name]
		if isBareKey(name) {
			fmt.Fprintf(&b, "[%s]\n", name)
		} else {
			fmt.Fprintf(&b, "[%q]\n", name)
		}
		fmt.Fprintf(&b, "path = %q\n", a.Path)
		if len(a.Subdirs) > 0 {
			b.WriteString("\n")
			if isBareKey(name) {
				fmt.Fprintf(&b, "[%s.subdirs]\n", name)
			} else {
				fmt.Fprintf(&b, "[%q.subdirs]\n", name)
			}
			keys := make([]string, 0, len(a.Subdirs))
			for k := range a.Subdirs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if isBareKey(k) {
					fmt.Fprintf(&b, "%s = %q\n", k, a.Subdirs[k])
				} else {
					fmt.Fprintf(&b, "%q = %q\n", k, a.Subdirs[k])
				}
			}
		}
		b.WriteString("\n")
	}

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

func isBareKey(s string) bool {
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
