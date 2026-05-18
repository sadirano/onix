package store

import (
	"os"
	"strings"
	"testing"
	"testing/quick"
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
		"":         "empty",
		"   ":      "empty",
		"foo bar":  "whitespace",
		"foo\tbar": "whitespace",
		"foo\nbar": "whitespace",
		"foo/bar":  "path",
		"foo\\bar": "path",
		"foo@bar":  "@",
		"\x00bar":  "control",
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

// TestValidateNames_Property asserts the roadmap invariant for the name
// validators: every input is either rejected with a kind-named error, or
// accepted *and* round-trips through Save → Load → Lookup. There is no third
// state where the validator accepts a name the store can't actually serve.
// Random rune strings catch the "we forgot a rune" gaps that the table tests
// can't reach by construction.
func TestValidateNames_Property(t *testing.T) {
	t.Run("alias_roundtrips_or_rejected", func(t *testing.T) {
		f := func(name string) bool {
			if err := ValidateAliasName(name); err != nil {
				return strings.Contains(err.Error(), "alias")
			}
			dir := t.TempDir()
			s := &Store{Aliases: map[string]Alias{}}
			s.Set(name, Alias{Path: "C:/x"})
			if err := SaveStore(dir, s); err != nil {
				return false
			}
			loaded, err := LoadStore(dir)
			if err != nil {
				return false
			}
			got, ok := loaded.Lookup(name)
			return ok && got.Path == "C:/x"
		}
		if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
			t.Error(err)
		}
	})

	t.Run("segment_validator_matches_spec", func(t *testing.T) {
		// Segment names live as `[[contexts]] segment = ...` keys in
		// segments.toml; an independently-derived spec is the cheapest way
		// to surface a "validator-forgot-a-rune" gap.
		f := func(name string) bool {
			err := ValidateSegmentName(name)
			legal := isLegalName(name)
			switch {
			case legal && err != nil:
				return false
			case !legal && err == nil:
				return false
			case err != nil && !strings.Contains(err.Error(), "segment"):
				return false
			}
			return true
		}
		if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
			t.Error(err)
		}
	})
}

// isLegalName mirrors validateName's rule set independently so the property
// test compares the validator against a separately-derived spec.
func isLegalName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == '@' || r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
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

func TestExpandTilde(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/abs/path", "/abs/path"},
		{"C:/abs/path", "C:/abs/path"},
		{"~/foo", ""}, // Needs HOME env
	}
	t.Setenv("HOME", "/home/user")
	t.Setenv("USERPROFILE", "/home/user") // Windows fallback
	for _, tc := range tests {
		got := ExpandTilde(tc.in)
		if tc.in == "~/foo" {
			if !strings.HasPrefix(got, "/home/user") {
				t.Errorf("ExpandTilde(~/foo) = %q, want prefix /home/user", got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("ExpandTilde(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStore_LoadBadTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(AliasesPath(dir), []byte(`invalid [ TOML`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadStore(dir)
	if err == nil {
		t.Fatal("expected error for bad TOML, got nil")
	}
}
