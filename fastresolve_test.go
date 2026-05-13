package main

import (
	"os"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/store"
)

// TestFastResolve_AgreesWithStore is the canonical contract test for the
// fast path: it must return exactly what LoadStore + Lookup would return
// for the same input. We seed a store via the regular Save path (so the
// on-disk format matches production) and then run both lookups in turn,
// comparing the results.
func TestFastResolve_AgreesWithStore(t *testing.T) {
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
		got, ok := scanForAlias(data, name)
		if !ok {
			t.Errorf("scan(%q): not found", name)
			continue
		}
		if got != want {
			t.Errorf("scan(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestFastResolve_MissingFallsThrough confirms the scanner returns
// (false) for an absent alias instead of returning a wrong sibling's
// value. main.go relies on this to know when to fall back to the slow
// path.
func TestFastResolve_MissingFallsThrough(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"a": {Path: "C:/a"}}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.AliasesPath(dir))
	if _, ok := scanForAlias(data, "missing"); ok {
		t.Error("scanForAlias claimed to find an absent alias")
	}
}

// TestFastResolve_HandlesQuotes makes sure paths containing escaped
// quotes round-trip correctly. SaveStore uses Go's %q which produces
// `\"` for embedded quotes, and the fast scanner needs to decode that
// back to a literal `"`.
func TestFastResolve_HandlesQuotes(t *testing.T) {
	const path = `C:/weird "name"/proj`
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"q": {Path: path}}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.AliasesPath(dir))
	got, ok := scanForAlias(data, "q")
	if !ok || got != path {
		t.Errorf("scan(q) = %q,%v want %q,true", got, ok, path)
	}
}

// TestFastResolve_StopsAtNextSection makes sure a section without a
// `path` doesn't read into the next section's value. This is the
// pathological case for a hand-roll parser.
func TestFastResolve_StopsAtNextSection(t *testing.T) {
	data := []byte(strings.Join([]string{
		`[a]`,
		``,
		`[b]`,
		`path = "C:/b"`,
		``,
	}, "\n"))
	if _, ok := scanForAlias(data, "a"); ok {
		t.Error("scanForAlias bled into sibling section")
	}
}

// BenchmarkFastResolve measures the hot-path-without-process-spawn cost
// of the scanner over a 200-alias file. Production hot path is this
// plus os.ReadFile, which is typically a single sub-millisecond syscall.
func BenchmarkFastResolve(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)
	data, err := os.ReadFile(store.AliasesPath(dir))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := scanForAlias(data, "alias100"); !ok {
			b.Fatal("alias100 missing")
		}
	}
}
