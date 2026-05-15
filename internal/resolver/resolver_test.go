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
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Resolve(dir, tc.in, nil)
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
