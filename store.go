package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Store is the on-disk alias database. One TOML file, all aliases. Keeping
// everything in a single file means the hot path is a single read + parse
// rather than the per-alias-file scheme v1 used.
//
// Layout on disk (aliases.toml):
//
//	[acme]
//	path = "C:/Users/dev/projects/acme"
//
//	[sms]
//	path = "D:/work/sms"
//
// We keep alias keys lowercase to make lookup case-insensitive without a
// per-call ToLower. The user can type any casing at the prompt.
//
// Aliases is not unmarshalled into via a struct tag — go-toml decodes
// directly into the map (see LoadStore). The struct is just a Go-level
// container so we can hang methods off it.
type Store struct {
	Aliases map[string]Alias
}

// Alias is one alias entry.
//
// Subdirs is a per-alias overlay over the global subdirs registry in
// segments.toml. When the user types `o docs@acme`, the resolver checks
// (in order) this map, then the global registry, then falls back to the
// literal segment name. Per-alias entries let one acme have docs ->
// "documentation" while a different alias maps docs -> "guides".
//
// The map key is the segment name as typed; values are joined into the
// path with filepath.Join, so they may contain slashes for multi-level
// shortcuts ("docs/internal", etc.).
type Alias struct {
	Path    string            `toml:"path"`
	Subdirs map[string]string `toml:"subdirs,omitempty"`
}

// LoadStore reads the aliases.toml at home/aliases.toml. When the file does
// not exist we return an empty (but non-nil) store so callers can use the
// same lookup paths regardless. Any other error (permission, bad TOML) is
// surfaced — silently swallowing a malformed file would lose data on the
// next Save.
func LoadStore(home string) (*Store, error) {
	p := aliasesPath(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Aliases: map[string]Alias{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	s := &Store{Aliases: map[string]Alias{}}
	// Decode straight into the map. go-toml v2 handles inline tables and
	// allocates the map for us, but we still want the field initialised for
	// the empty case above.
	if err := toml.Unmarshal(data, &s.Aliases); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	// Normalise keys to lowercase on load so the in-memory map matches the
	// canonical lookup form. If a user hand-edits the file with mixed case
	// we still find the entry without a second pass at lookup time.
	if needs, lowered := lowerKeys(s.Aliases); needs {
		s.Aliases = lowered
	}
	return s, nil
}

// Lookup returns the alias by name (case-insensitive). The bool is false
// when the alias is not present — callers should error out rather than
// silently fall through, because v1's "try as a literal path" fallback
// made typos hard to diagnose.
func (s *Store) Lookup(name string) (Alias, bool) {
	a, ok := s.Aliases[strings.ToLower(strings.TrimSpace(name))]
	return a, ok
}

// Set adds or replaces an alias. The path is stored as-is (the caller is
// expected to have already absolutised and cleaned it) so round-tripping
// through Save+Load is lossless.
func (s *Store) Set(name string, a Alias) {
	if s.Aliases == nil {
		s.Aliases = map[string]Alias{}
	}
	s.Aliases[strings.ToLower(strings.TrimSpace(name))] = a
}

// Delete removes an alias. Returns true when something was removed so the
// caller can print a different message for "not found".
func (s *Store) Delete(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if _, ok := s.Aliases[key]; !ok {
		return false
	}
	delete(s.Aliases, key)
	return true
}

// Names returns the alias names in sorted order — used by `onix list` and
// (later) by shell-completion. Cheap because the map is small.
func (s *Store) Names() []string {
	out := make([]string, 0, len(s.Aliases))
	for k := range s.Aliases {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SaveStore writes the store to home/aliases.toml atomically: encode into
// a temp file in the same directory, then rename. This avoids a half-written
// file if the process dies mid-write, which would otherwise corrupt the
// user's entire alias list.
func SaveStore(home string, s *Store) error {
	p := aliasesPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}

	// We want stable output for diffing and review, so encode by hand into
	// a sorted name order instead of relying on go-toml's map iteration.
	// Per-alias subdir entries land in a nested [name.subdirs] subtable so
	// each alias's block reads top-down: identity (path) → overrides.
	var b strings.Builder
	b.WriteString("# onix aliases — edit with care, prefer `onix add` / `onix rm`\n\n")
	for _, name := range s.Names() {
		a := s.Aliases[name]
		fmt.Fprintf(&b, "[%s]\n", name)
		fmt.Fprintf(&b, "path = %q\n", a.Path)
		if len(a.Subdirs) > 0 {
			b.WriteString("\n")
			fmt.Fprintf(&b, "[%s.subdirs]\n", name)
			// Sort the subdir keys too so re-saves produce identical
			// output for unchanged input — that's what keeps the file
			// reviewable in git.
			keys := make([]string, 0, len(a.Subdirs))
			for k := range a.Subdirs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "%s = %q\n", k, a.Subdirs[k])
			}
		}
		b.WriteString("\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(p), ".aliases.*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename.
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

// lowerKeys returns a fresh map with keys lowercased when at least one key
// in the input wasn't already lowercase. When everything is already canonical
// it returns (false, nil) so the caller can avoid the allocation.
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
