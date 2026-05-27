package resolver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/store"
)

// samePath returns true if a and b resolve to the same path, accounting
// for case sensitivity and slash direction.
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.ToSlash(a), filepath.ToSlash(b))
}

func TestResolve_Errors(t *testing.T) {
	dir := t.TempDir()
	_ = store.SaveStore(dir, &store.Store{Aliases: map[string]store.Alias{
		"acme": {Path: "C:/acme"},
	}})

	t.Run("unknown alias", func(t *testing.T) {
		_, err := Resolve(dir, "nope", nil, nil, nil)
		if err == nil {
			t.Error("expected error for unknown alias")
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		_, err := Resolve(dir, "foo/bar", nil, nil, nil)
		if err == nil {
			t.Error("expected error for invalid alias name")
		}
	})
}

func TestResolve_Basic(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"a": {Path: "C:/a"}}}
	_ = store.SaveStore(dir, s)

	t.Run("fast path", func(t *testing.T) {
		got, err := Resolve(dir, "a", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.FromSlash("C:/a") {
			t.Errorf("got %q, want C:/a", got)
		}
	})

	t.Run("slow path", func(t *testing.T) {
		got, err := Resolve(dir, "a", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.FromSlash("C:/a") {
			t.Errorf("got %q, want C:/a", got)
		}
	})
}

func TestResolve_Segmented_UnknownNoPrompt(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{Path: "C:/acme"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(dir, "mystery@acme", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for undefined segment")
	}
	if !strings.Contains(err.Error(), "mystery") {
		t.Errorf("error should mention segment name: %v", err)
	}
}
