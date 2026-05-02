package installer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
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

// Add appends a new [[module]] entry to config and immediately installs it.
// repo is "user/repo" or "github.com/user/repo". alias overrides the command
// name; if empty, the repo's last segment is used.
func Add(repo, alias string, cfg *config.Config) error {
	repo = normalizeRepo(repo)
	if !strings.Contains(repo, "/") {
		return fmt.Errorf("invalid repo %q — expected format: user/repo or github.com/user/repo", repo)
	}

	name := alias
	if name == "" {
		parts := strings.Split(repo, "/")
		name = parts[len(parts)-1]
	}
	if name == "" {
		return fmt.Errorf("invalid repo %q — could not determine module name", repo)
	}

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
	fmt.Printf("Added %q (%s)\n", name, repo)
	if err := Install(name, cfg); err != nil {
		fmt.Printf("  ! install failed: %v\n  Retry with: onix install %s\n", err, name)
	}
	return nil
}

// Remove uninstalls a module: deletes its directory, its .cmd wrapper, and
// removes its entry from config.
func Remove(name string, cfg *config.Config) error {
	var removedRepo string
	found := false
	kept := make([]config.Module, 0, len(cfg.Modules))
	for _, m := range cfg.Modules {
		if strings.EqualFold(m.EffectiveName(), name) {
			found = true
			removedRepo = m.Repo
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
	modDir := moduleDir(removedRepo)
	if err := os.RemoveAll(modDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove module dir: %w", err)
	}

	// Remove .cmd wrapper(s) — handles both single and multi-entry modules.
	if err := removeModuleWrappers(name); err != nil {
		return fmt.Errorf("remove wrappers: %w", err)
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
		if IsInstalled(m.Repo) {
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

	names := make([]string, 0, len(config.Shortcuts))
	for name := range config.Shortcuts {
		names = append(names, name)
	}
	sort.Strings(names)
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
	srcDir := config.ModuleDir(m.Repo)
	binName := config.RepoBinName(m.Repo) + ".exe"
	binPath := filepath.Join(srcDir, binName)

	if err := cloneOrUpdate(repoURL, m.Ref, srcDir); err != nil {
		return err
	}
	if err := buildGo(srcDir, binPath); err != nil {
		return err
	}

	manifest, err := loadManifest(srcDir)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	entries := resolveEntries(manifest, m.Entries)

	if len(entries) == 0 {
		// Single-entry path: one wrapper named after the module alias.
		if err := checkCmdConflict(name, name, ""); err != nil {
			return err
		}
		if err := createWrapper(name); err != nil {
			return err
		}
	} else {
		// Multi-entry path: one wrapper per entry.
		for _, e := range entries {
			cmdName := e.EffectiveCmd()
			if err := checkCmdConflict(cmdName, name, e.Name); err != nil {
				return err
			}
			if err := createEntryWrapper(name, e.Name, cmdName); err != nil {
				return err
			}
		}
	}

	fmt.Printf("  ✓ %s -> %s\n", name, binPath)
	return nil
}

func cloneOrUpdate(repoURL, ref, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if err := syncRef(dir, ref); err != nil {
			fmt.Printf("  ! could not reach remote for %s (%v) — building from cached source\n", filepath.Base(dir), err)
		}
		return nil
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

	// Pull only when on a branch; symbolic-ref fails on detached HEAD (tag/commit checkout).
	cmd := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	if out, err := cmd.Output(); err == nil {
		return runGit(dir, "pull", "--ff-only", "origin", strings.TrimSpace(string(out)))
	}
	return nil
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
	mod := cfg.FindModule(name)

	repo := "sadirano/onix-" + name
	if mod != nil {
		repo = mod.Repo
	}

	if IsInstalled(repo) {
		return nil
	}

	if mod == nil {
		fmt.Printf("Module %q is not installed or declared in config.\nAdd %q and install? [y/N] ", name, repo)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			return fmt.Errorf("module %q not installed — add it with: onix add <repo>", name)
		}
		if err := Add(repo, "", cfg); err != nil {
			return fmt.Errorf("add module %q: %w", name, err)
		}
	} else {
		fmt.Printf("Module %q is declared but not installed. Install now? [y/N] ", name)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			return fmt.Errorf("module %q not installed — run: onix install %s", name, name)
		}
	}
	// Note: ReadString errors (e.g. closed stdin) are intentionally ignored;
	// an empty/failed read produces an empty string which fails the "y" check,
	// so the prompt safely rejects non-interactive invocations.
	return Install(name, cfg)
}

// IsInstalled reports whether the module binary for the given repo exists on disk.
// repo is "user/repo" (or the full GitHub URL — it will be normalized).
func IsInstalled(repo string) bool {
	name := repoName(repo)
	bin := filepath.Join(moduleDir(repo), name+".exe")
	_, err := os.Stat(bin)
	return err == nil
}

// normalizeRepo delegates to config.NormalizeRepo.
func normalizeRepo(repo string) string { return config.NormalizeRepo(repo) }

// moduleDir delegates to config.ModuleDir.
func moduleDir(repo string) string { return config.ModuleDir(repo) }

// repoName delegates to config.RepoBinName.
func repoName(repo string) string { return config.RepoBinName(repo) }

// ---------------------------------------------------------------------------
// Entry-point helpers
// ---------------------------------------------------------------------------

// manifestToml is the structure of an onix.toml manifest in a module source dir.
type manifestToml struct {
	Entries []config.Entry `toml:"entry"`
}

// loadManifest reads onix.toml from srcDir and returns its entry list.
// Returns nil (not an error) when the file is absent.
func loadManifest(srcDir string) ([]config.Entry, error) {
	p := filepath.Join(srcDir, "onix.toml")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}
	var m manifestToml
	if _, err := toml.DecodeFile(p, &m); err != nil {
		return nil, err
	}
	return m.Entries, nil
}

// resolveEntries applies user overrides (from config) on top of the manifest.
// If manifest is empty, returns nil (no multi-entry mode).
// For each manifest entry, if overrides contains a matching name with a
// non-empty Cmd, that Cmd is used; otherwise the entry's Name is used as Cmd.
func resolveEntries(manifest []config.Entry, overrides []config.Entry) []config.Entry {
	if len(manifest) == 0 {
		return nil
	}
	overrideMap := make(map[string]string, len(overrides))
	for _, o := range overrides {
		if o.Cmd != "" {
			overrideMap[o.Name] = o.Cmd
		}
	}
	out := make([]config.Entry, len(manifest))
	for i, e := range manifest {
		cmd := e.Name
		if ov, ok := overrideMap[e.Name]; ok {
			cmd = ov
		} else if e.Cmd != "" {
			cmd = e.Cmd
		}
		out[i] = config.Entry{Name: e.Name, Cmd: cmd}
	}
	return out
}

// extractCmdVar parses a line of the form  set "VARNAME=value"  from content
// and returns value. Returns "" when the variable is not found.
func extractCmdVar(content, varName string) string {
	prefix := `set "` + varName + `=`
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.TrimSuffix(val, `"`)
			return val
		}
	}
	return ""
}

// checkCmdConflict returns an error when the .cmd file at BinDir/cmdName.cmd
// already exists and is owned by a different module/entry.
// Returns nil when the file is absent or when it is owned by (moduleName, entryName).
func checkCmdConflict(cmdName, moduleName, entryName string) error {
	p := filepath.Join(config.BinDir(), cmdName+".cmd")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing wrapper %s: %w", cmdName+".cmd", err)
	}
	content := string(data)
	existingModule := extractCmdVar(content, "ONIX_MODULE")
	existingEntry := extractCmdVar(content, "ONIX_ENTRY")
	if existingModule == moduleName && existingEntry == entryName {
		return nil // same owner — safe to overwrite
	}
	if existingModule != "" {
		return fmt.Errorf("cmd %q is already owned by module %q (entry %q) — remove that module first", cmdName, existingModule, existingEntry)
	}
	return nil // not an onix-managed wrapper; allow overwrite
}

// createEntryWrapper writes a .cmd wrapper that sets both ONIX_MODULE and ONIX_ENTRY.
func createEntryWrapper(moduleName, entryName, cmdName string) error {
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
	content := fmt.Sprintf(
		"@echo off\r\nsetlocal\r\nset \"ONIX_MODULE=%s\"\r\nset \"ONIX_ENTRY=%s\"\r\n\"%s\" %%*\r\nset \"ONIX_EXIT=%%ERRORLEVEL%%\"\r\nendlocal & exit /b %%ONIX_EXIT%%\r\n",
		moduleName, entryName, onixExe,
	)
	return os.WriteFile(filepath.Join(config.BinDir(), cmdName+".cmd"), []byte(content), 0o644)
}

// removeModuleWrappers removes all .cmd files in BinDir whose ONIX_MODULE
// variable matches moduleName. This handles both single-entry and multi-entry
// modules transparently.
func removeModuleWrappers(moduleName string) error {
	binDir := config.BinDir()
	entries, err := os.ReadDir(binDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".cmd") {
			continue
		}
		p := filepath.Join(binDir, de.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if extractCmdVar(string(data), "ONIX_MODULE") == moduleName {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", de.Name(), err)
			}
		}
	}
	return nil
}
