package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/sadirano/onix/internal/store"
)

// BenchmarkHotPath_LoadAndLookup is the measurement of the resolve hot path.
func BenchmarkHotPath_LoadAndLookup(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := store.LoadStore(dir)
		if err != nil {
			b.Fatal(err)
		}
		if _, ok := s.Lookup("alias100"); !ok {
			b.Fatal("alias100 missing")
		}
	}
}

// BenchmarkHotPath_LookupOnly measures pure lookup cost.
func BenchmarkHotPath_LookupOnly(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)
	s, err := store.LoadStore(dir)
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

// seedStore writes a fresh aliases.toml with n aliases.
func seedStore(tb testing.TB, dir string, n int) {
	tb.Helper()
	s := &store.Store{Aliases: make(map[string]store.Alias, n)}
	for i := 0; i < n; i++ {
		s.Set(fmt.Sprintf("alias%d", i),
			store.Alias{Path: filepath.ToSlash(fmt.Sprintf("C:/projects/team-%d/service-%d", i/10, i))})
	}
	if err := store.SaveStore(dir, s); err != nil {
		tb.Fatal(err)
	}
}
