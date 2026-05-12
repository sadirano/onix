package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkHotPath_LoadAndLookup is the canonical measurement of the resolve
// hot path: read aliases.toml from disk, parse it, look up one alias. Every
// `o acme` keystroke pays exactly this cost (plus the Go runtime startup,
// which is not visible in an in-process benchmark — see scripts/bench.ps1
// for end-to-end measurement once we have it).
//
// Target: <1ms with 200 aliases on a warm filesystem cache. If this regresses
// past 1ms we look at the regression before merging.
func BenchmarkHotPath_LoadAndLookup(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := LoadStore(dir)
		if err != nil {
			b.Fatal(err)
		}
		if _, ok := s.Lookup("alias100"); !ok {
			b.Fatal("alias100 missing")
		}
	}
}

// BenchmarkHotPath_LookupOnly measures pure lookup cost on an already-loaded
// store. Helpful for spotting if the lookup itself is a bottleneck under
// daemon mode (where the load happens once, then thousands of lookups).
func BenchmarkHotPath_LookupOnly(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)
	s, err := LoadStore(dir)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := s.Lookup("alias100"); !ok {
			b.Fatal("alias100 missing")
		}
	}
}

// seedStore writes a fresh aliases.toml with n aliases. We use a fixed naming
// scheme (alias0..aliasN-1) and a fake but plausible-looking Windows path so
// the parsed bytes are representative of a real user's file.
func seedStore(tb testing.TB, dir string, n int) {
	tb.Helper()
	s := &Store{Aliases: make(map[string]Alias, n)}
	for i := 0; i < n; i++ {
		s.Set(fmt.Sprintf("alias%d", i),
			Alias{Path: filepath.ToSlash(fmt.Sprintf("C:/projects/team-%d/service-%d", i/10, i))})
	}
	if err := SaveStore(dir, s); err != nil {
		tb.Fatal(err)
	}
}
