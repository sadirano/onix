package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/store"
)

// TestScanForAlias_AgreesWithStore is the canonical contract test for the
// fast path: it must return exactly what LoadStore + Lookup would return
// for the same input.
func TestScanForAlias_AgreesWithStore(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	cases := map[string]string{
		"acme":  "C:/projects/acme",
		"sms":   "D:/work/sms",
		"funky": "/var/lib/some path",
	}
	for k, v := range cases {
		s.Set(k, store.Alias{Path: v})
	}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(store.AliasesPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for name, want := range cases {
		got, ok := ScanForAlias(data, name)
		if !ok {
			t.Errorf("scan(%q): not found", name)
			continue
		}
		if got != want {
			t.Errorf("scan(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestScanForAlias_MissingFallsThrough confirms the scanner returns
// (false) for an absent alias.
func TestScanForAlias_MissingFallsThrough(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"a": {Path: "C:/a"}}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.AliasesPath(dir))
	if _, ok := ScanForAlias(data, "missing"); ok {
		t.Error("ScanForAlias claimed to find an absent alias")
	}
}

// TestScanForAlias_HandlesQuotes makes sure paths containing escaped
// quotes round-trip correctly.
func TestScanForAlias_HandlesQuotes(t *testing.T) {
	const path = `C:/weird "name"/proj`
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"q": {Path: path}}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.AliasesPath(dir))
	got, ok := ScanForAlias(data, "q")
	if !ok || got != path {
		t.Errorf("scan(q) = %q,%v want %q,true", got, ok, path)
	}
}

// TestScanForAlias_StopsAtNextSection makes sure a section without a
// `path` doesn't read into the next section's value.
func TestScanForAlias_StopsAtNextSection(t *testing.T) {
	data := []byte(strings.Join([]string{
		`[a]`,
		``,
		`[b]`,
		`path = "C:/b"`,
		``,
	}, "\n"))
	if _, ok := ScanForAlias(data, "a"); ok {
		t.Error("ScanForAlias bled into sibling section")
	}
}

// BenchmarkScanForAlias measures the hot-path cost of the scanner.
func BenchmarkScanForAlias(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)
	data, err := os.ReadFile(store.AliasesPath(dir))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := ScanForAlias(data, "alias100"); !ok {
			b.Fatal("alias100 missing")
		}
	}
}

func seedStore(t testing.TB, dir string, count int) {
	s := &store.Store{Aliases: map[string]store.Alias{}}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("alias%d", i)
		s.Set(name, store.Alias{Path: "C:/path/to/" + name})
	}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
}

// TestResolve_Segmented captures the multi-segment resolution rules.
func TestResolve_Segmented(t *testing.T) {
	dir := t.TempDir()

	// Aliases: one with a per-alias subdir override.
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{
		Path:    "C:/projects/acme",
		Subdirs: map[string]string{"docs": "doc-internal"},
	})
	s.Set("vanilla", store.Alias{Path: "C:/projects/vanilla"})
	if err := store.SaveStore(dir, s); err != nil {
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
		{"docs@acme", "C:/projects/acme/doc-internal"},
		{"docs@vanilla", "C:/projects/vanilla/documentation"},
		{"random@acme", "C:/projects/acme/random"},
		{"src@docs@vanilla", "C:/projects/vanilla/documentation/source"},
		// Inline value is parsed but currently ignored — resolution uses
		// the segment name only. Segments-spec PR 4 wires the value through.
		{"docs:ignored@acme", "C:/projects/acme/doc-internal"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Resolve(dir, tc.in, nil, nil)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			want := filepath.FromSlash(tc.want)
			if got != want {
				t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, want)
			}
		})
	}
}

func TestResolve_Basic(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"a": {Path: "C:/a"}}}
	_ = store.SaveStore(dir, s)

	t.Run("fast path", func(t *testing.T) {
		got, err := Resolve(dir, "a", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.FromSlash("C:/a") {
			t.Errorf("got %q, want C:/a", got)
		}
	})

	t.Run("slow path", func(t *testing.T) {
		// Delete file to force slow path (or just use non-fast alias name if any)
		// Wait, slow path is also triggered if Resolve reads full store.
		// Resolve always tries fast path first.
		got, err := Resolve(dir, "A", nil, nil) // Case insensitive
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.FromSlash("C:/a") {
			t.Errorf("got %q, want C:/a", got)
		}
	})
}

// TestResolve_FuzzyMatch_DistanceLimit guards against the loose-match
// regression where short typos selected far-off candidates ("sync" → "bin",
// distance 3). The new limit is shorter-length/3, capped at 3.
func TestResolve_FuzzyMatch_DistanceLimit(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{
		"bin":  {Path: "C:/bin"},
		"onix": {Path: "C:/onix"},
		"play": {Path: "C:/play"},
	}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}

	selected := ""
	selector := func(opts []string) string {
		if len(opts) > 0 {
			selected = opts[0]
		}
		return ""
	}

	// "sync" is distance 3 from "bin" but distance 4 from "onix" / "play".
	// Under the new limit (shorter/3 = 1 for shorter=3, capped to 1) NONE
	// of these are close enough to suggest. The selector should not fire.
	_, err := Resolve(dir, "sync", nil, selector)
	if err == nil {
		t.Errorf("expected error for unknown alias 'sync', got nil")
	}
	if selected != "" {
		t.Errorf("selector should not have been called for sync; got candidate %q", selected)
	}

	// Sanity: a real typo (one transposition) should still trigger the selector.
	selected = ""
	_, err = Resolve(dir, "onxi", nil, selector)
	if err == nil {
		t.Errorf("expected error for unknown alias 'onxi' when selector returns empty")
	}
	if selected != "onix" {
		t.Errorf("real typo 'onxi' should suggest 'onix', got %q", selected)
	}
}

// TestResolve_NilSelector_BypassesFuzzy ensures that passing a nil selector
// completely skips the did-you-mean machinery. The hot-path fastResolve
// relies on this when --no-prompt is set, otherwise the cmd-wrapper's
// `for /f` capture would auto-pick a candidate.
func TestResolve_NilSelector_BypassesFuzzy(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{
		"onix": {Path: "C:/onix"},
	}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(dir, "onxi", nil, nil)
	if err == nil {
		t.Errorf("expected error when selector is nil and alias is unknown")
	}
	if !strings.Contains(err.Error(), "unknown alias") {
		t.Errorf("expected 'unknown alias' error, got %v", err)
	}
}

func TestResolve_WithPrompter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new-path")

	t.Run("success", func(t *testing.T) {
		prompter := func(name string) string {
			return target
		}
		got, err := Resolve(dir, "new", prompter, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != target {
			t.Errorf("got %q, want %q", got, target)
		}
		// Verify it was saved
		s, _ := store.LoadStore(dir)
		if a, ok := s.Lookup("new"); !ok || filepath.ToSlash(a.Path) != filepath.ToSlash(target) {
			t.Errorf("alias not saved correctly: %+v", a)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		prompter := func(name string) string {
			return ""
		}
		_, err := Resolve(dir, "cancelled", prompter, nil)
		if err != ErrCancelled {
			t.Fatalf("expected ErrCancelled, got %v", err)
		}
	})
}
