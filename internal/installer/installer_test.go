package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

// homeSetup redirects the onix home to an isolated temp directory for the
// duration of the test, preventing any writes to the real ~/.onix.
// On Windows, os.UserHomeDir reads USERPROFILE, so overriding it is sufficient.
func homeSetup(t *testing.T) {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
}

// ---------------------------------------------------------------------------
// normalizeRepo
// ---------------------------------------------------------------------------

func TestNormalizeRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sadirano/onix-sg", "sadirano/onix-sg"},
		{"github.com/sadirano/onix-sg", "sadirano/onix-sg"},
		{"https://github.com/sadirano/onix-sg", "sadirano/onix-sg"},
		{"http://github.com/sadirano/onix-sg", "sadirano/onix-sg"},
		{"vendor/repo", "vendor/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := config.NormalizeRepo(tt.input); got != tt.want {
				t.Errorf("config.NormalizeRepo(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsInstalled
// ---------------------------------------------------------------------------

func TestIsInstalled_True(t *testing.T) {
	homeSetup(t)
	const repo = "user/mymod"
	binDir := filepath.Join(config.ModulesDir(), "user", "mymod")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "mymod.exe"), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled(repo) {
		t.Errorf("IsInstalled(%q) = false, want true", repo)
	}
}

func TestIsInstalled_False(t *testing.T) {
	homeSetup(t)
	if IsInstalled("user/nonexistent") {
		t.Error("IsInstalled(\"user/nonexistent\") = true, want false")
	}
}

// ---------------------------------------------------------------------------
// Add
// ---------------------------------------------------------------------------

func TestAdd_NewModule(t *testing.T) {
	homeSetup(t)
	cfg := &config.Config{}
	// Add saves config before attempting install. The install will fail because
	// the repo doesn't exist, but the config entry must still be persisted so the
	// user can retry with `onix install mymod`.
	err := Add("user/mymod", "", cfg)
	if err == nil {
		t.Fatal("expected Add to return an error when install fails, got nil")
	}
	saved, err2 := config.Load()
	if err2 != nil {
		t.Fatalf("config.Load: %v", err2)
	}
	if saved.FindModule("mymod") == nil {
		t.Error("expected module \"mymod\" in saved config even after install failure")
	}
}

func TestAdd_DuplicateModule(t *testing.T) {
	homeSetup(t)
	cfg := &config.Config{
		Modules: []config.Module{
			{Name: "mymod", Repo: "user/mymod", Enabled: true},
		},
	}
	err := Add("user/mymod", "", cfg)
	if err == nil {
		t.Fatal("expected error for duplicate module, got nil")
	}
	if !strings.Contains(err.Error(), "already in config") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAdd_InvalidRepo(t *testing.T) {
	cfg := &config.Config{}
	if err := Add("notarepo", "", cfg); err == nil {
		t.Fatal("expected error for bare module name, got nil")
	}
}

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

func TestInstall_NotInConfig(t *testing.T) {
	homeSetup(t)
	cfg := &config.Config{}
	if err := Install("missing", cfg); err == nil {
		t.Fatal("expected error for module not in config, got nil")
	}
}

func TestInstall_DisabledModule(t *testing.T) {
	homeSetup(t)
	cfg := &config.Config{
		Modules: []config.Module{
			{Name: "mymod", Repo: "user/mymod", Ref: "main", Enabled: false},
		},
	}
	if err := Install("mymod", cfg); err != nil {
		t.Fatalf("Install on disabled module: %v", err)
	}
	if IsInstalled("user/mymod") {
		t.Error("disabled module must not be installed")
	}
}

// ---------------------------------------------------------------------------
// InstallAll
// ---------------------------------------------------------------------------

func TestInstallAll_Empty(t *testing.T) {
	homeSetup(t)
	if err := InstallAll(&config.Config{}); err != nil {
		t.Errorf("InstallAll on empty config: %v", err)
	}
}

func TestInstallAll_HaltsOnFirstError(t *testing.T) {
	t.Skip("requires a local git + Go fixture — implement as integration test")
}

// ---------------------------------------------------------------------------
// Offline install
// ---------------------------------------------------------------------------

func TestOfflineInstall_ExistingClone(t *testing.T) {
	// When a module's source dir already has a .git repo but the remote is
	// unreachable, cloneOrUpdate must warn and return nil so the build can
	// proceed from the cached source.
	homeSetup(t)

	srcDir := filepath.Join(config.ModulesDir(), "user", "mymod")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Init a real but remote-less git repo so that "git fetch origin" fails
	// immediately with "No remote named 'origin'" rather than a network timeout.
	if out, err := exec.Command("git", "init", srcDir).CombinedOutput(); err != nil {
		t.Skipf("git init failed (%v): %s — skipping offline test", err, out)
	}

	err := cloneOrUpdate("https://127.0.0.1:1/nonexistent.git", "main", srcDir)
	if err != nil {
		t.Errorf("cloneOrUpdate with unreachable remote on existing clone: got %v, want nil", err)
	}
}

func TestOfflineInstall_NoClone(t *testing.T) {
	// When there is no local source at all and the remote is unreachable, the
	// install must fail with a hard error — there is nothing to build from.
	homeSetup(t)
	srcDir := filepath.Join(config.ModulesDir(), "user", "nomod")
	if err := cloneOrUpdate("https://127.0.0.1:1/nonexistent.git", "main", srcDir); err == nil {
		t.Error("expected error when remote unreachable and no local clone exists")
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUpdate_NotInConfig(t *testing.T) {
	homeSetup(t)
	if err := Update("missing", &config.Config{}); err == nil {
		t.Fatal("expected error updating module not in config, got nil")
	}
}

func TestUpdate_Single(t *testing.T) {
	t.Skip("requires a local git + Go fixture — implement as integration test")
}

func TestUpdate_All(t *testing.T) {
	t.Skip("requires a local git + Go fixture — implement as integration test")
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestRemove_NotFound(t *testing.T) {
	homeSetup(t)
	if err := Remove("missing", &config.Config{}); err == nil {
		t.Fatal("expected error removing unknown module, got nil")
	}
}

func TestRemove_RemovesFilesAndConfig(t *testing.T) {
	homeSetup(t)

	cfg := &config.Config{
		Modules: []config.Module{
			{Name: "mymod", Repo: "user/mymod", Ref: "main", Enabled: true},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	// Simulate an installed module: create source dir and .cmd wrapper.
	srcDir := filepath.Join(config.ModulesDir(), "user", "mymod")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(config.BinDir(), "mymod.cmd")
	if err := os.MkdirAll(config.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate an onix-generated .cmd wrapper (must contain ONIX_MODULE so
	// removeModuleWrappers can identify it).
	wrapperContent := "@echo off\r\nsetlocal\r\nset \"ONIX_MODULE=mymod\"\r\n\"onix.exe\" %*\r\nendlocal\r\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapperContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Remove("mymod", cfg); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Error("source dir should have been removed")
	}
	if _, err := os.Stat(wrapperPath); !os.IsNotExist(err) {
		t.Error(".cmd wrapper should have been removed")
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.FindModule("mymod") != nil {
		t.Error("module entry should have been removed from config")
	}
}

// ---------------------------------------------------------------------------
// EnsureInstalled
// ---------------------------------------------------------------------------

func TestEnsureInstalled_AlreadyInstalled(t *testing.T) {
	homeSetup(t)
	const name = "mymod"
	binDir := filepath.Join(config.ModulesDir(), "user", name)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, name+".exe"), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Modules: []config.Module{
			{Name: name, Repo: "user/mymod", Ref: "main", Enabled: true},
		},
	}
	if err := EnsureInstalled(name, cfg); err != nil {
		t.Errorf("EnsureInstalled on already-installed module: %v", err)
	}
}

func TestEnsureInstalled_NonInteractiveDeny(t *testing.T) {
	// A closed stdin produces an empty read, which fails the "y" check and must
	// cause EnsureInstalled to return an error without attempting install.
	homeSetup(t)
	cfg := &config.Config{
		Modules: []config.Module{
			{Name: "mymod", Repo: "user/mymod", Ref: "main", Enabled: true},
		},
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	if err := EnsureInstalled("mymod", cfg); err == nil {
		t.Error("expected error from non-interactive stdin, got nil")
	}
}
