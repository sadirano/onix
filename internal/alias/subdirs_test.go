package alias

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSubdirFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "subdirs.env")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write subdirs.env: %v", err)
	}
	return p
}

func TestResolveSubdir(t *testing.T) {
	t.Run("local file resolves name", func(t *testing.T) {
		base := t.TempDir()
		writeSubdirFile(t, base, "an=anexos\n")
		if got := ResolveSubdir("an", base); got != "anexos" {
			t.Errorf("got %q, want %q", got, "anexos")
		}
	})

	t.Run("local lookup is case-insensitive", func(t *testing.T) {
		base := t.TempDir()
		writeSubdirFile(t, base, "AN=anexos\n")
		if got := ResolveSubdir("an", base); got != "anexos" {
			t.Errorf("got %q, want %q", got, "anexos")
		}
	})

	t.Run("global registry resolves when local has no match", func(t *testing.T) {
		globalPath := filepath.Join(t.TempDir(), "subdirs.env")
		if err := os.WriteFile(globalPath, []byte("doc=documentacao\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		v, ok := lookupSubdir("doc", globalPath)
		if !ok || v != "documentacao" {
			t.Errorf("global lookup: got (%q, %v), want (\"documentacao\", true)", v, ok)
		}
		// Confirm "doc" is not found in an unrelated local file.
		base := t.TempDir()
		writeSubdirFile(t, base, "an=anexos\n")
		_, found := lookupSubdir("doc", filepath.Join(base, "subdirs.env"))
		if found {
			t.Error("unexpectedly found doc in local file that only has 'an'")
		}
	})

	t.Run("local overrides global for same key", func(t *testing.T) {
		base := t.TempDir()
		writeSubdirFile(t, base, "cfg=local-cfg\n")
		// Simulate: if we look up local first and find it, global is never checked.
		v, ok := lookupSubdir("cfg", filepath.Join(base, "subdirs.env"))
		if !ok || v != "local-cfg" {
			t.Errorf("got (%q, %v), want (\"local-cfg\", true)", v, ok)
		}
	})

	t.Run("returns literal name when not in either registry", func(t *testing.T) {
		base := t.TempDir()
		if got := ResolveSubdir("outros", base); got != "outros" {
			t.Errorf("got %q, want %q", got, "outros")
		}
	})

	t.Run("missing local file is silent", func(t *testing.T) {
		base := t.TempDir() // no subdirs.env written
		if got := ResolveSubdir("an", base); got != "an" {
			t.Errorf("got %q, want %q", got, "an")
		}
	})
}

func TestParseSubdirFile(t *testing.T) {
	t.Run("BOM stripped", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "subdirs.env")
		// Write file with UTF-8 BOM.
		if err := os.WriteFile(p, []byte("\xef\xbb\xbfkey=val\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := parseSubdirFile(p)
		if m["key"] != "val" {
			t.Errorf("BOM not stripped: got map %v", m)
		}
	})

	t.Run("CRLF handled", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "subdirs.env")
		if err := os.WriteFile(p, []byte("a=b\r\nc=d\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := parseSubdirFile(p)
		if m["a"] != "b" || m["c"] != "d" {
			t.Errorf("CRLF not handled: got map %v", m)
		}
	})

	t.Run("comments and blank lines skipped", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "subdirs.env")
		content := "# comment\n\nan=anexos\n\n# another\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		m := parseSubdirFile(p)
		if len(m) != 1 || m["an"] != "anexos" {
			t.Errorf("unexpected map: %v", m)
		}
	})

	t.Run("missing file returns nil", func(t *testing.T) {
		m := parseSubdirFile("/nonexistent/path/subdirs.env")
		if m != nil {
			t.Errorf("expected nil, got %v", m)
		}
	})
}
