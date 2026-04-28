package installer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// Install clones (or updates) the repo for the named module, builds the
// binary, and writes the .cmd wrapper into ~/.onix/bin/.
func Install(name string, cfg *config.Config) error {
	mod := cfg.FindModule(name)
	if mod == nil {
		return fmt.Errorf("module %q not found in config — add it first with: onix add <repo>", name)
	}
	if !mod.Enabled {
		fmt.Printf("  skip %s (disabled)\n", name)
		return nil
	}
	return installModule(mod)
}

// InstallAll installs every enabled module declared in config.
func InstallAll(cfg *config.Config) error {
	if len(cfg.Modules) == 0 {
		fmt.Println("No modules declared in config. Add one with: onix add <user/repo>")
		return nil
	}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !m.Enabled {
			fmt.Printf("  skip  %s (disabled)\n", m.EffectiveName())
			continue
		}
		fmt.Printf("  install %s (%s)\n", m.EffectiveName(), m.Repo)
		if err := installModule(m); err != nil {
			return fmt.Errorf("install %s: %w", m.EffectiveName(), err)
		}
	}
	return nil
}

// Add appends a new [[module]] entry to config without installing it.
// repo is "user/repo" or "github.com/user/repo".
func Add(repo string, cfg *config.Config) error {
	repo = normalizeRepo(repo)
	parts := strings.Split(repo, "/")
	name := parts[len(parts)-1]

	if cfg.FindModule(name) != nil {
		return fmt.Errorf("module %q already in config", name)
	}

	cfg.Modules = append(cfg.Modules, config.Module{
		Name:    name,
		Repo:    repo,
		Ref:     "main",
		Enabled: true,
	})

	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("Added %q (%s) — run `onix install %s` to build and wire it up.\n", name, repo, name)
	return nil
}

// Remove uninstalls a module: deletes its directory, its .cmd wrapper, and
// removes its entry from config.
func Remove(name string, cfg *config.Config) error {
	found := false
	kept := make([]config.Module, 0, len(cfg.Modules))
	for _, m := range cfg.Modules {
		if strings.EqualFold(m.EffectiveName(), name) {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		return fmt.Errorf("module %q not found in config", name)
	}
	cfg.Modules = kept

	if err := config.Save(cfg); err != nil {
		return err
	}

	// Remove binary directory.
	modDir := filepath.Join(config.ModulesDir(), name)
	if err := os.RemoveAll(modDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove module dir: %w", err)
	}

	// Remove .cmd wrapper.
	wrapper := filepath.Join(config.BinDir(), name+".cmd")
	if err := os.Remove(wrapper); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove wrapper: %w", err)
	}

	fmt.Printf("Removed %s.\n", name)
	return nil
}

// Update pulls the latest source and rebuilds a single module (or all if name
// is empty).
func Update(name string, cfg *config.Config) error {
	if name != "" {
		mod := cfg.FindModule(name)
		if mod == nil {
			return fmt.Errorf("module %q not in config", name)
		}
		fmt.Printf("  update %s\n", mod.EffectiveName())
		return installModule(mod)
	}
	// Update all.
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !m.Enabled {
			continue
		}
		fmt.Printf("  update %s\n", m.EffectiveName())
		if err := installModule(m); err != nil {
			return fmt.Errorf("update %s: %w", m.EffectiveName(), err)
		}
	}
	return nil
}

// List prints all declared modules and whether they are installed.
func List(cfg *config.Config) {
	if len(cfg.Modules) == 0 {
		fmt.Println("No modules declared. Add one with: onix add <user/repo>")
		return
	}
	fmt.Printf("%-16s  %-32s  %-8s  %s\n", "NAME", "REPO", "REF", "STATUS")
	fmt.Println(strings.Repeat("-", 72))
	for _, m := range cfg.Modules {
		name := m.EffectiveName()
		ref := m.Ref
		if ref == "" {
			ref = "main"
		}
		status := "not installed"
		if IsInstalled(name) {
			status = "installed"
		}
		if !m.Enabled {
			status = "disabled"
		}
		fmt.Printf("%-16s  %-32s  %-8s  %s\n", name, m.Repo, ref, status)
	}
}

// Init creates the onix directory structure and writes a starter config.
func Init() error {
	dirs := []string{config.Dir(), config.ModulesDir(), config.BinDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	cfgPath := config.Path()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfgPath, []byte(config.Starter), 0o644); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", cfgPath)
	} else {
		fmt.Printf("Config already exists: %s\n", cfgPath)
	}

	fmt.Printf(`
Onix initialized at %s

Add the following directory to your PATH:
  %s

Then declare modules in:
  %s

And run:
  onix install
`, config.Dir(), config.BinDir(), cfgPath)

	return nil
}

// InstallShortcuts writes cmd wrappers in ~/.onix/bin/ for built-in shortcuts.
func InstallShortcuts() error {
	onixExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	onixExe, err = filepath.Abs(onixExe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	binDir := config.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	names := []string{"o", "c", "s", "n", "y", "f", "r", "sg", "sga", "ff"}
	var warnings []string
	for _, name := range names {
		legacyExe := filepath.Join(binDir, name+".exe")
		if err := os.Remove(legacyExe); err != nil && !os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("%s.exe still in use: %v", name, err))
		}

		if err := createShortcutWrapper(name, config.Shortcuts[name], onixExe, binDir); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s.cmd skipped: %v", name, err))
			fmt.Printf("  ! %s.cmd (skipped: %v)\n", name, err)
			continue
		}
		fmt.Printf("  %s.cmd\n", name)
	}
	fmt.Printf("Shortcuts installed in %s\n", binDir)
	if len(warnings) > 0 {
		fmt.Println("Shortcut warnings:")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
		fmt.Println("Close related shells and run `onix shortcuts` again to refresh all shortcuts.")
	}

	if err := addBinToUserPath(binDir); err != nil {
		fmt.Printf("Warning: could not update PATH automatically: %v\n", err)
		fmt.Printf("Add manually: %s\n", binDir)
	}
	return nil
}

// addBinToUserPath adds dir to the user-scoped PATH in the Windows registry.
// Uses PowerShell's [Environment]::SetEnvironmentVariable so the change
// persists across sessions without requiring a reboot or admin rights.
func addBinToUserPath(dir string) error {
	script := `
$dir = '` + dir + `'
$current = [Environment]::GetEnvironmentVariable("Path", "User")
$parts = $current -split ";" | Where-Object { $_ -ne "" }
if ($parts -contains $dir) {
    Write-Host "PATH already contains $dir"
} else {
    $new = ($parts + $dir) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $new, "User")
    Write-Host "Added to PATH: $dir"
    Write-Host "Restart your terminal for the change to take effect."
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createShortcutWrapper(name, flag, onixExe, binDir string) error {
	extra := ""
	if strings.TrimSpace(flag) != "" {
		extra = " " + flag
	}
	content := fmt.Sprintf("@echo off\r\nsetlocal\r\n\"%s\" %%*%s\r\nset \"ONIX_EXIT=%%ERRORLEVEL%%\"\r\nendlocal & exit /b %%ONIX_EXIT%%\r\n", onixExe, extra)
	return os.WriteFile(filepath.Join(binDir, name+".cmd"), []byte(content), 0o644)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func installModule(m *config.Module) error {
	name := m.EffectiveName()
	repoURL := "https://github.com/" + normalizeRepo(m.Repo)
	srcDir := filepath.Join(config.ModulesDir(), name)
	binPath := filepath.Join(srcDir, name+".exe")

	if err := cloneOrUpdate(repoURL, m.Ref, srcDir); err != nil {
		return err
	}
	if err := buildGo(srcDir, binPath); err != nil {
		return err
	}
	if err := createWrapper(name); err != nil {
		return err
	}
	fmt.Printf("  ✓ %s -> %s\n", name, binPath)
	return nil
}

func cloneOrUpdate(repoURL, ref, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return syncRef(dir, ref)
	}

	// Directory exists but has no .git — treat as a local module, skip clone.
	if _, err := os.Stat(dir); err == nil {
		fmt.Printf("  (local) %s — skipping clone\n", filepath.Base(dir))
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}

	cmd := exec.Command("git", "clone", "--depth=1", repoURL, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	return syncRef(dir, ref)
}

func syncRef(dir, ref string) error {
	ref = strings.TrimSpace(ref)
	if err := runGit(dir, "fetch", "--tags", "--prune", "origin"); err != nil {
		return err
	}

	if ref == "" {
		return runGit(dir, "pull", "--ff-only")
	}

	if err := runGit(dir, "checkout", ref); err != nil {
		if err2 := runGit(dir, "checkout", "-B", ref, "origin/"+ref); err2 != nil {
			return fmt.Errorf("checkout ref %q: %w", ref, err2)
		}
	}

	branch, err := currentBranch(dir)
	if err != nil {
		return err
	}
	if branch != "HEAD" {
		if err := runGit(dir, "pull", "--ff-only", "origin", branch); err != nil {
			return err
		}
	}
	return nil
}

func currentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runGit(dir string, args ...string) error {
	fullArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildGo(srcDir, outPath string) error {
	// Check if this is a Go project.
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
		return fmt.Errorf("no go.mod found in %s — only Go modules are supported currently", srcDir)
	}

	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createWrapper(name string) error {
	if err := os.MkdirAll(config.BinDir(), 0o755); err != nil {
		return err
	}

	onixExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve onix executable: %w", err)
	}
	onixExe, err = filepath.Abs(onixExe)
	if err != nil {
		return fmt.Errorf("resolve onix executable path: %w", err)
	}
	content := fmt.Sprintf("@echo off\r\nsetlocal\r\nset \"ONIX_MODULE=%s\"\r\n\"%s\" %%*\r\nset \"ONIX_EXIT=%%ERRORLEVEL%%\"\r\nendlocal & exit /b %%ONIX_EXIT%%\r\n", name, onixExe)

	return os.WriteFile(filepath.Join(config.BinDir(), name+".cmd"), []byte(content), 0o644)
}

// EnsureInstalled checks whether the named module is installed. If not, it
// prompts the user to confirm installation. If the module is not yet declared
// in config it infers the default repo as sadirano/onix-<name>.
func EnsureInstalled(name string, cfg *config.Config) error {
	if IsInstalled(name) {
		return nil
	}
	if cfg.FindModule(name) == nil {
		guessedRepo := "sadirano/onix-" + name
		fmt.Printf("Module %q is not installed or declared in config.\nAdd %q and install? [y/N] ", name, guessedRepo)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			return fmt.Errorf("module %q not installed — add it with: onix add <repo>", name)
		}
		if err := Add(guessedRepo, cfg); err != nil {
			return fmt.Errorf("add module %q: %w", name, err)
		}
	} else {
		fmt.Printf("Module %q is declared but not installed. Install now? [y/N] ", name)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			return fmt.Errorf("module %q not installed — run: onix install %s", name, name)
		}
	}
	return Install(name, cfg)
}

// IsInstalled reports whether the named module binary exists on disk.
func IsInstalled(name string) bool {
	bin := filepath.Join(config.ModulesDir(), name, name+".exe")
	_, err := os.Stat(bin)
	return err == nil
}

// normalizeRepo strips any leading "github.com/" or URL scheme so the result
// is always "user/repo".
func normalizeRepo(repo string) string {
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	return repo
}
