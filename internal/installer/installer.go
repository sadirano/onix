package installer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	lua "github.com/yuin/gopher-lua"

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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func installModule(m *config.Module) error {
	name := m.EffectiveName()
	repoURL := "https://github.com/" + config.NormalizeRepo(m.Repo)
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

	fmt.Printf("  ok %s -> %s\n", name, binPath)
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

// EnsureInstalled checks whether the named module is installed. If not, it
// prompts the user to confirm installation. The module must be declared in
// config.lua first.
func EnsureInstalled(name string, cfg *config.Config) error {
	mod := cfg.FindModule(name)
	if mod == nil {
		return fmt.Errorf("module %q is not declared in config.lua — add it to the modules table, then run: onix install %s", name, name)
	}

	if IsInstalled(mod.Repo) {
		return nil
	}

	fmt.Printf("Module %q is declared but not installed. Install now? [y/N] ", name)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		return fmt.Errorf("module %q not installed — run: onix install %s", name, name)
	}
	return Install(name, cfg)
}

// IsInstalled reports whether the module binary for the given repo exists on disk.
// repo is "user/repo" (or the full GitHub URL — it will be normalized).
func IsInstalled(repo string) bool {
	name := config.RepoBinName(repo)
	bin := filepath.Join(config.ModuleDir(repo), name+".exe")
	_, err := os.Stat(bin)
	return err == nil
}


// ---------------------------------------------------------------------------
// Entry-point helpers
// ---------------------------------------------------------------------------

// loadManifest reads onix.lua from srcDir and returns its entry list.
// Returns nil (not an error) when the file is absent.
func loadManifest(srcDir string) ([]config.Entry, error) {
	p := filepath.Join(srcDir, "onix.lua")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}

	L := lua.NewState()
	defer L.Close()

	if err := L.DoFile(p); err != nil {
		return nil, fmt.Errorf("onix.lua: %w", err)
	}

	tbl, ok := L.Get(-1).(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("onix.lua must return a table")
	}

	et, ok := tbl.RawGetString("entries").(*lua.LTable)
	if !ok {
		return nil, nil
	}

	var entries []config.Entry
	n := et.MaxN()
	for i := 1; i <= n; i++ {
		t, ok := et.RawGetInt(i).(*lua.LTable)
		if !ok {
			continue
		}
		name := luaManifestStr(t, "name")
		cmd := luaManifestStr(t, "cmd")
		entries = append(entries, config.Entry{Name: name, Cmd: cmd})
	}
	return entries, nil
}

func luaManifestStr(t *lua.LTable, key string) string {
	if s, ok := t.RawGetString(key).(lua.LString); ok {
		return string(s)
	}
	return ""
}

// resolveEntries applies user overrides (from config) on top of the manifest.
// If manifest is empty, returns nil (no multi-entry mode).
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
