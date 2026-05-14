package store

import (
	"os"
	"strings"
	"testing"
)

// TestStore_RoundTrip writes a few aliases, reads them back, and confirms
// the lookup, name list, and on-disk format are all stable across a save+load
// cycle. This is the spine of the hot path — if it breaks, everything breaks.
func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	s := &Store{Aliases: map[string]Alias{}}
	s.Set("acme", Alias{Path: "C:/projects/acme"})
	s.Set("sms", Alias{Path: "D:/work/sms"})
	if err := SaveStore(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := map[string]string{"acme": "C:/projects/acme", "sms": "D:/work/sms"}
	for k, p := range want {
		a, ok := loaded.Lookup(k)
		if !ok {
			t.Errorf("lookup %q: not found", k)
			continue
		}
		if a.Path != p {
			t.Errorf("lookup %q: path = %q, want %q", k, a.Path, p)
		}
	}

	names := loaded.Names()
	if len(names) != 2 || names[0] != "acme" || names[1] != "sms" {
		t.Errorf("Names() = %v, want [acme sms]", names)
	}
}

// TestStore_LookupCaseInsensitive guards the documented promise that
// `o ACME` and `o acme` resolve the same alias. Mixed-case keys on disk
// also normalise on load.
func TestStore_LookupCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Aliases: map[string]Alias{}}
	s.Set("Acme", Alias{Path: "C:/projects/acme"})
	if err := SaveStore(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, name := range []string{"acme", "ACME", "Acme", "aCmE"} {
		if _, ok := loaded.Lookup(name); !ok {
			t.Errorf("lookup %q: not found (case-insensitive should match)", name)
		}
	}
}

// TestStore_LoadMissingReturnsEmpty confirms that the first-run experience
// (no aliases.toml on disk) doesn't crash the binary. Resolve will still
// fail with "unknown alias" but Load itself should not.
func TestStore_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s == nil {
		t.Fatal("Load returned nil store")
	}
	if len(s.Aliases) != 0 {
		t.Fatalf("expected empty aliases, got %v", s.Aliases)
	}
}

// TestStore_DeleteUnknown confirms Delete returns false when the alias
// isn't present, so the CLI can surface the right message.
func TestStore_DeleteUnknown(t *testing.T) {
	s := &Store{Aliases: map[string]Alias{"acme": {Path: "C:/x"}}}
	if s.Delete("nope") {
		t.Error("Delete(nope) returned true on a missing alias")
	}
	if !s.Delete("acme") {
		t.Error("Delete(acme) returned false on a present alias")
	}
	if _, ok := s.Lookup("acme"); ok {
		t.Error("alias persisted after Delete")
	}
}

// TestValidateNames locks the character rules that prevent unresolvable
// aliases (and the segment-name counterpart). The rule set is shared, so
// both functions are exercised with the same fixtures.
func TestValidateNames(t *testing.T) {
	t.Run("alias", func(t *testing.T) {
		runValidatorTable(t, ValidateAliasName, "alias")
	})
	t.Run("segment", func(t *testing.T) {
		runValidatorTable(t, ValidateSegmentName, "segment")
	})
}

func runValidatorTable(t *testing.T, fn func(string) error, kind string) {
	t.Helper()
	ok := []string{
		"acme",
		"acme-2",
		"acme_v2",
		"FooBar",
		"a",
	}
	for _, name := range ok {
		if err := fn(name); err != nil {
			t.Errorf("%s %q: unexpected error: %v", kind, name, err)
		}
	}
	bad := map[string]string{
		"":            "empty",
		"   ":         "empty",
		"foo bar":     "whitespace",
		"foo\tbar":    "whitespace",
		"foo\nbar":    "whitespace",
		"foo/bar":     "path",
		"foo\\bar":    "path",
		"foo@bar":     "@",
		"\x00bar":     "control",
	}
	for name, hint := range bad {
		err := fn(name)
		if err == nil {
			t.Errorf("%s %q (%s): expected error, got nil", kind, name, hint)
			continue
		}
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("%s %q: error %q should name the kind", kind, name, err)
		}
	}
}

// TestStore_AtomicWrite confirms that the saved file ends up readable and
// that we don't leave temp .aliases.*.toml droppings behind on success.
// A scan of the directory is the cheapest way to verify the cleanup path.
func TestStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Aliases: map[string]Alias{"a": {Path: "C:/a"}}}
	if err := SaveStore(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".aliases.") && strings.HasSuffix(name, ".toml") {
			t.Errorf("temp file left behind after save: %s", name)
		}
	}
	// And the canonical file should exist.
	if _, err := os.Stat(AliasesPath(dir)); err != nil {
		t.Errorf("aliases.toml not present after save: %v", err)
	}
}

// TestStore_SubdirsRoundTrip locks the on-disk shape for per-alias subdir overrides.
func TestStore_SubdirsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Aliases: map[string]Alias{}}
	s.Set("acme", Alias{
		Path:    "C:/projects/acme",
		Subdirs: map[string]string{"docs": "doc-internal", "src": "source-acme"},
	})
	if err := SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(AliasesPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the file has both the path line and the subdirs subtable.
	got := string(body)
	for _, want := range []string{`[acme]`, `path = `, `C:/projects/acme`, `[acme.subdirs]`, `docs = `, `doc-internal`, `src = `, `source-acme`} {
		if !strings.Contains(got, want) {
			t.Errorf("aliases.toml missing %q\n--- file ---\n%s", want, got)
		}
	}

	// Reload and confirm the map survived.
	s2, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := s2.Lookup("acme")
	if !ok {
		t.Fatal("acme missing after reload")
	}
	if a.Subdirs["docs"] != "doc-internal" || a.Subdirs["src"] != "source-acme" {
		t.Errorf("subdirs round-trip lost data: %+v", a.Subdirs)
	}
}
