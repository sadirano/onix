package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseSegmentedAlias locks the parser contract: the alias is the
// token *after* the last '@', segments are everything before in
// left-to-right order, and empty segments (from leading or duplicated
// '@') are dropped.
func TestParseSegmentedAlias(t *testing.T) {
	tests := []struct {
		in       string
		wantSegs []string
		wantAls  string
	}{
		{"acme", nil, "acme"},
		{"docs@acme", []string{"docs"}, "acme"},
		{"task@client@place", []string{"task", "client"}, "place"},
		{"a@b@c@d", []string{"a", "b", "c"}, "d"},
		{"@trailing", nil, "trailing"},               // leading @ drops empty seg
		{"a@@b", []string{"a"}, "b"},                 // duplicate @ skips empty seg
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			segs, als := ParseSegmentedAlias(tc.in)
			if !reflect.DeepEqual(segs, tc.wantSegs) {
				t.Errorf("segments = %v, want %v", segs, tc.wantSegs)
			}
			if als != tc.wantAls {
				t.Errorf("alias = %q, want %q", als, tc.wantAls)
			}
		})
	}
}

// TestResolveSegment_PrecedenceChain captures the three-tier lookup
// rules: per-alias subdirs first, then global subdirs, then literal
// fallback. Each subtest exercises one tier.
func TestResolveSegment_PrecedenceChain(t *testing.T) {
	aliasSubs := map[string]string{"docs": "doc-internal"}
	globalSubs := map[string]string{"docs": "documentation", "src": "source"}

	t.Run("per-alias wins over global", func(t *testing.T) {
		got := ResolveSegment("docs", aliasSubs, globalSubs)
		if got != "doc-internal" {
			t.Errorf("docs = %q, want doc-internal", got)
		}
	})

	t.Run("global wins when no per-alias", func(t *testing.T) {
		got := ResolveSegment("src", aliasSubs, globalSubs)
		if got != "source" {
			t.Errorf("src = %q, want source", got)
		}
	})

	t.Run("literal fallback when unmapped", func(t *testing.T) {
		got := ResolveSegment("undocumented", aliasSubs, globalSubs)
		if got != "undocumented" {
			t.Errorf("undocumented = %q, want literal fallback", got)
		}
	})

	t.Run("case-insensitive match", func(t *testing.T) {
		got := ResolveSegment("SRC", aliasSubs, globalSubs)
		if got != "source" {
			t.Errorf("SRC = %q, want source (case-insensitive lookup)", got)
		}
	})

	t.Run("empty per-alias value falls through to global", func(t *testing.T) {
		got := ResolveSegment("docs", map[string]string{"docs": " "}, globalSubs)
		if got != "documentation" {
			t.Errorf("docs = %q, want documentation (empty override skipped)", got)
		}
	})
}

// TestSegments_LoadRoundTrip writes a segments.toml by hand and confirms
// the loader builds the map go-toml exposes.
func TestSegments_LoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `
[subdirs]
docs = "documentation"
src  = "source"
ts   = "tests"
`
	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sf, err := LoadSegments(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := sf.Subdirs["docs"]; got != "documentation" {
		t.Errorf("docs = %q, want documentation", got)
	}
	if got := sf.Subdirs["ts"]; got != "tests" {
		t.Errorf("ts = %q, want tests", got)
	}
}

// TestSegments_LoadMissingReturnsEmpty is the first-run path.
func TestSegments_LoadMissingReturnsEmpty(t *testing.T) {
	sf, err := LoadSegments(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sf == nil || len(sf.Subdirs) != 0 {
		t.Fatalf("expected empty segments, got %+v", sf)
	}
}

// TestResolveSegmentedToPath is the end-to-end path-builder test. We
// stand up an aliases.toml with a per-alias override, a segments.toml
// with a global mapping, and assert the walk produces the right path
// for several segment shapes.
func TestResolveSegmentedToPath(t *testing.T) {
	dir := t.TempDir()

	// Aliases: one with a per-alias subdir override.
	store := &Store{Aliases: map[string]Alias{}}
	store.Set("acme", Alias{
		Path:    "C:/projects/acme",
		Subdirs: map[string]string{"docs": "doc-internal"},
	})
	store.Set("vanilla", Alias{Path: "C:/projects/vanilla"})
	if err := SaveStore(dir, store); err != nil {
		t.Fatal(err)
	}

	// Global subdirs.
	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(`
[subdirs]
docs = "documentation"
src  = "source"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in   string
		want string
	}{
		// Per-alias override beats the global mapping.
		{"docs@acme", "C:/projects/acme/doc-internal"},
		// Global mapping when no per-alias override.
		{"docs@vanilla", "C:/projects/vanilla/documentation"},
		// Literal fallback for unregistered segments.
		{"random@acme", "C:/projects/acme/random"},
		// Multi-segment: innermost first.
		{"src@docs@vanilla", "C:/projects/vanilla/documentation/source"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := resolveSegmentedToPath(dir, tc.in)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			want := filepath.FromSlash(tc.want)
			if got != want {
				t.Errorf("resolve(%q) = %q, want %q", tc.in, got, want)
			}
		})
	}
}

// TestStore_SubdirsRoundTrip locks the on-disk shape for per-alias
// subdir overrides: a [<alias>.subdirs] subtable per alias, keys sorted
// for stable diffs.
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
	body, err := os.ReadFile(filepath.Join(dir, "aliases.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the file has both the path line and the subdirs subtable.
	got := string(body)
	for _, want := range []string{`[acme]`, `path = "C:/projects/acme"`, `[acme.subdirs]`, `docs = "doc-internal"`, `src = "source-acme"`} {
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

