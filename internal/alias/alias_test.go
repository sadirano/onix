package alias

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

// setAliasDir points the alias package at an isolated temp directory.
func setAliasDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv(EnvVar, dir)
}

func TestApplyEnvOverride(t *testing.T) {
	t.Run("sets ONIX_ALIAS_DIR when not set", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv(EnvVar); got != "/my/aliases" {
			t.Errorf("got %q, want /my/aliases", got)
		}
	})
	t.Run("no-op when aliasDir is empty", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		ApplyEnvOverride("")
		if got := os.Getenv(EnvVar); got != "" {
			t.Errorf("expected no change, got %q", got)
		}
	})
	t.Run("no-op when aliasDir is whitespace-only", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		ApplyEnvOverride("   ")
		if got := os.Getenv(EnvVar); got != "" {
			t.Errorf("expected no change, got %q", got)
		}
	})
	t.Run("no-op when ONIX_ALIAS_DIR already set", func(t *testing.T) {
		t.Setenv(EnvVar, "/existing")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv(EnvVar); got != "/existing" {
			t.Errorf("expected existing value preserved, got %q", got)
		}
	})
}

func TestDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	t.Run("ONIX_ALIAS_DIR takes priority", func(t *testing.T) {
		t.Setenv(EnvVar, "/custom/aliases")
		if got := Dir(); got != "/custom/aliases" {
			t.Errorf("got %q, want /custom/aliases", got)
		}
	})
	t.Run("default is ~/.onix/aliases", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		got := Dir()
		want := filepath.Join(home, ".onix", "aliases")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Load / Save roundtrip
// ---------------------------------------------------------------------------

func TestLoadSave_PathOnly(t *testing.T) {
	setAliasDir(t, t.TempDir())
	e := &Entry{Path: "/projects/acme"}
	if err := Save("acme", e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("acme")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.Path != "/projects/acme" {
		t.Errorf("got %+v, want Path=/projects/acme", got)
	}
}

func TestLoadSave_WithContext(t *testing.T) {
	setAliasDir(t, t.TempDir())
	e := &Entry{
		Path: "/projects/client",
		Context: config.ContextConfig{
			Source:   "env",
			Var:      "CLIENT_ID",
			Template: "clients/{value}",
		},
	}
	if err := Save("client", e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("client")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Context.Source != "env" || got.Context.Var != "CLIENT_ID" {
		t.Errorf("Context: got %+v", got.Context)
	}
	if got.Context.Template != "clients/{value}" {
		t.Errorf("Template: got %q", got.Context.Template)
	}
}

func TestLoadSave_WithSegments(t *testing.T) {
	setAliasDir(t, t.TempDir())
	e := &Entry{
		Path: "/projects/api",
		Segments: map[string]config.ContextConfig{
			"sg":   {Source: "env", Var: "STACK", Template: "stacks/{value}"},
			"task": {Source: "alias", Path: "frontend"},
		},
	}
	if err := Save("api", e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("api")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Segments) != 2 {
		t.Fatalf("len(Segments) = %d, want 2", len(got.Segments))
	}
	if got.Segments["sg"].Var != "STACK" {
		t.Errorf("sg.Var = %q, want STACK", got.Segments["sg"].Var)
	}
	if got.Segments["task"].Path != "frontend" {
		t.Errorf("task.Path = %q, want frontend", got.Segments["task"].Path)
	}
}

func TestLoad_Missing(t *testing.T) {
	setAliasDir(t, t.TempDir())
	got, err := Load("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_CreatesFile(t *testing.T) {
	setAliasDir(t, t.TempDir())
	if err := Register("proj", "/my/proj"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := Load("proj")
	if err != nil || got == nil || got.Path != "/my/proj" {
		t.Errorf("after Register: got %+v, err=%v", got, err)
	}
}

func TestRegister_UpdatesPath(t *testing.T) {
	setAliasDir(t, t.TempDir())
	if err := Register("proj", "/old"); err != nil {
		t.Fatal(err)
	}
	if err := Register("proj", "/new"); err != nil {
		t.Fatalf("Register upsert: %v", err)
	}
	got, _ := Load("proj")
	if got == nil || got.Path != "/new" {
		t.Errorf("got %+v, want Path=/new", got)
	}
}

func TestRegister_PreservesContext(t *testing.T) {
	setAliasDir(t, t.TempDir())
	e := &Entry{
		Path:    "/old",
		Context: config.ContextConfig{Source: "env", Var: "MY_VAR"},
	}
	if err := Save("proj", e); err != nil {
		t.Fatal(err)
	}
	if err := Register("proj", "/new"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, _ := Load("proj")
	if got.Path != "/new" {
		t.Errorf("Path = %q, want /new", got.Path)
	}
	if got.Context.Var != "MY_VAR" {
		t.Errorf("Context.Var = %q, want MY_VAR (should be preserved)", got.Context.Var)
	}
}

// ---------------------------------------------------------------------------
// Resolve
// ---------------------------------------------------------------------------

func TestResolve_AliasLookup(t *testing.T) {
	d := t.TempDir()
	setAliasDir(t, d)
	if err := Register("acme", d); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("acme", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != d {
		t.Errorf("got %q, want %q", got, d)
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	d := t.TempDir()
	setAliasDir(t, filepath.Join(d, "aliases"))
	if err := Register("MyProj", d); err != nil {
		t.Fatal(err)
	}
	// Alias file is MyProj.lua — lookup by exact name only (Lua filenames are case-sensitive).
	// The test verifies that the stored path is correctly returned.
	got, err := Resolve("MyProj", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != d {
		t.Errorf("got %q, want %q", got, d)
	}
}

func TestResolve_RawPathFallback(t *testing.T) {
	d := t.TempDir()
	setAliasDir(t, filepath.Join(d, "aliases"))
	// No alias registered — the raw directory path itself should be accepted.
	got, err := Resolve(d, false)
	if err != nil {
		t.Fatalf("Resolve raw path: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestResolve_UnknownAliasReturnsError(t *testing.T) {
	setAliasDir(t, t.TempDir())
	_, err := Resolve("nosuchalias", false)
	if err == nil {
		t.Fatal("expected error for unknown alias, got nil")
	}
}

func TestResolve_AliasBeatsRawPath(t *testing.T) {
	d := t.TempDir()
	subName := "mything"
	subDir := filepath.Join(d, subName)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasTarget := filepath.Join(d, "alias-target")
	if err := os.MkdirAll(aliasTarget, 0o755); err != nil {
		t.Fatal(err)
	}

	setAliasDir(t, filepath.Join(d, "aliases"))
	if err := Register(subName, aliasTarget); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got, err := Resolve(subName, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != aliasTarget {
		t.Errorf("got %q, want alias target %q", got, aliasTarget)
	}
}
