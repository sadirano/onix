package segments

import (
	"os"
	"reflect"
	"testing"
)

// TestParseSegmentedAlias locks the parser contract.
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
		{"@trailing", nil, "trailing"},
		{"a@@b", []string{"a"}, "b"},
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

// TestResolveSegment_PrecedenceChain captures the lookup rules.
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

// TestSegments_LoadRoundTrip writes a segments.toml by hand.
func TestSegments_LoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `
[subdirs]
docs = "documentation"
src  = "source"
ts   = "tests"
`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
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

func TestLoadSegments_BadTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`invalid [ TOML`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSegments(dir)
	if err == nil {
		t.Fatal("expected error for bad TOML, got nil")
	}
}

func TestLookupCaseInsensitive_Nil(t *testing.T) {
	v, ok := lookupCaseInsensitive(nil, "foo")
	if ok || v != "" {
		t.Errorf("lookupCaseInsensitive(nil) = %q, %v, want \"\", false", v, ok)
	}
}
