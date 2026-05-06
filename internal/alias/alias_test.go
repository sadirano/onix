package alias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setAliasFile points the alias package at a specific file for the duration of
// the test by setting ONIX_ALIAS_FILE and clearing the higher-priority vars.
func setAliasFile(t *testing.T, path string) {
	t.Helper()
	t.Setenv("ONIX_ENV", "")
	t.Setenv("ONIX_ALIAS_FILE", path)
}

func TestApplyEnvOverride(t *testing.T) {
	clearAliasEnv := func(t *testing.T) {
		t.Helper()
		for _, k := range []string{"ONIX_ENV", "ONIX_ALIAS_FILE"} {
			t.Setenv(k, "")
		}
	}

	t.Run("sets ONIX_ALIAS_FILE when no env var is set", func(t *testing.T) {
		clearAliasEnv(t)
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "/my/aliases" {
			t.Errorf("got %q, want /my/aliases", got)
		}
	})
	t.Run("no-op when aliasFile is empty", func(t *testing.T) {
		clearAliasEnv(t)
		ApplyEnvOverride("")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("expected no change, got %q", got)
		}
	})
	t.Run("no-op when aliasFile is whitespace-only", func(t *testing.T) {
		clearAliasEnv(t)
		ApplyEnvOverride("   ")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("expected no change, got %q", got)
		}
	})
	t.Run("no-op when ONIX_ENV already set", func(t *testing.T) {
		clearAliasEnv(t)
		t.Setenv("ONIX_ENV", "/already/set")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("ONIX_ALIAS_FILE should be untouched, got %q", got)
		}
	})
	t.Run("no-op when ONIX_ALIAS_FILE already set", func(t *testing.T) {
		clearAliasEnv(t)
		t.Setenv("ONIX_ALIAS_FILE", "/existing")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "/existing" {
			t.Errorf("expected existing value preserved, got %q", got)
		}
	})
}

func TestFilePath(t *testing.T) {
	// Isolate from actual user home to prevent picking up existing .onix files.
	tempHome := t.TempDir()
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)

	t.Run("ONIX_ENV takes priority", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "/custom/path")
		t.Setenv("ONIX_ALIAS_FILE", "/other/path")
		if got := FilePath(); got != "/custom/path" {
			t.Errorf("got %q, want /custom/path", got)
		}
	})
	t.Run("ONIX_ALIAS_FILE second priority", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "")
		t.Setenv("ONIX_ALIAS_FILE", "/alias/file")
		if got := FilePath(); got != "/alias/file" {
			t.Errorf("got %q, want /alias/file", got)
		}
	})
	t.Run("default path contains .onix/aliases", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "")
		t.Setenv("ONIX_ALIAS_FILE", "")
		got := FilePath()
		if !strings.HasSuffix(got, filepath.Join(".onix", "aliases")) {
			t.Errorf("got %q, expected suffix %q", got, filepath.Join(".onix", "aliases"))
		}
	})
	t.Run("whitespace-only env var falls through", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "   ")
		t.Setenv("ONIX_ALIAS_FILE", "/alias/file")
		if got := FilePath(); got != "/alias/file" {
			t.Errorf("got %q, want /alias/file", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	aliases, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected empty map, got %v", aliases)
	}
}

func TestLoad_BOMStripping(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	// UTF-8 BOM followed by a valid alias line.
	content := "\xef\xbb\xbfmyalias=/some/path\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	aliases, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if aliases["myalias"] != "/some/path" {
		t.Errorf("got %v, want myalias=/some/path", aliases)
	}
}

func TestLoad_CRLFNormalisation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "a=/path/a\r\nb=/path/b\r\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	aliases, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if aliases["a"] != "/path/a" {
		t.Errorf("a: got %q, want /path/a", aliases["a"])
	}
	if aliases["b"] != "/path/b" {
		t.Errorf("b: got %q, want /path/b", aliases["b"])
	}
}

func TestLoad_MalformedLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "good=/path\nnoequalssign\n# comment\n=nokey\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	aliases, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if aliases["good"] != "/path" {
		t.Errorf("good: got %q, want /path", aliases["good"])
	}
	if len(aliases) != 1 {
		t.Errorf("expected exactly 1 entry, got %v", aliases)
	}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_InsertNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	setAliasFile(t, p)

	if err := Register("proj", "/my/proj"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	aliases, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if aliases["proj"] != "/my/proj" {
		t.Errorf("got %q, want /my/proj", aliases["proj"])
	}
}

func TestRegister_UpsertExistingKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	setAliasFile(t, p)

	if err := Register("proj", "/old"); err != nil {
		t.Fatal(err)
	}
	if err := Register("proj", "/new"); err != nil {
		t.Fatalf("Register upsert: %v", err)
	}

	aliases, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if aliases["proj"] != "/new" {
		t.Errorf("got %q, want /new", aliases["proj"])
	}
	// Ensure no duplicate line exists.
	data, _ := os.ReadFile(p)
	count := strings.Count(string(data), "proj=")
	if count != 1 {
		t.Errorf("expected exactly one proj= line, found %d", count)
	}
}

func TestRegister_PreservesComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	initial := "# my comment\nfoo=/foo\n"
	if err := os.WriteFile(p, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	if err := Register("bar", "/bar"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "# my comment") {
		t.Error("comment was not preserved after Register")
	}
}

func TestRegister_WritesCRLF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	setAliasFile(t, p)

	if err := Register("x", "/x"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "\r\n") {
		t.Error("expected CRLF line endings in written file")
	}
}

func TestRegister_BOMInput(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	// Write a file with a UTF-8 BOM to simulate input from some editors.
	content := "\xef\xbb\xbfexisting=/some/path\r\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	// Register a new alias — must not corrupt the existing entry.
	if err := Register("newkey", "/new"); err != nil {
		t.Fatalf("Register on BOM file: %v", err)
	}

	aliases, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if aliases["existing"] != "/some/path" {
		t.Errorf("existing alias corrupted: got %q", aliases["existing"])
	}
	if aliases["newkey"] != "/new" {
		t.Errorf("newkey: got %q, want /new", aliases["newkey"])
	}
}

// ---------------------------------------------------------------------------
// Resolve
// ---------------------------------------------------------------------------

func TestResolve_AliasFileLookup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("myproj="+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	got, err := Resolve("myproj", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("MyProj="+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	got, err := Resolve("myproj", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestResolve_RawPathFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	// Empty alias file — raw path must be accepted.
	if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	got, err := Resolve(dir, false)
	if err != nil {
		t.Fatalf("Resolve raw path: %v", err)
	}
	// Result must be absolute.
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestResolve_AliasBeatsRawPath(t *testing.T) {
	// C6 fix: a name that matches both an alias AND a CWD-relative path must
	// resolve via the alias, not the filesystem path.
	dir := t.TempDir()
	// Create a sub-directory whose name matches our alias key.
	subName := "mything"
	subDir := filepath.Join(dir, subName)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasTarget := filepath.Join(dir, "alias-target")
	if err := os.MkdirAll(aliasTarget, 0o755); err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte(subName+"="+aliasTarget+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	// Change CWD into dir so that subName resolves as a relative path.
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got, err := Resolve(subName, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != aliasTarget {
		t.Errorf("got %q, want alias target %q (alias should beat raw path)", got, aliasTarget)
	}
}

func TestResolve_UnknownAliasReturnsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	setAliasFile(t, p)

	_, err := Resolve("nosuchalias", false)
	if err == nil {
		t.Fatal("expected error for unknown alias, got nil")
	}
}
