package installer

import (
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
	kept := cfg.Modules[:0]
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
		if isInstalled(name) {
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

	home, _ := os.UserHomeDir()
	fmt.Printf(`
Onix initialized at %s

Add the following directory to your PATH:
  %s

Then declare modules in:
  %s

And run:
  onix install
`, config.Dir(), config.BinDir(), cfgPath)

	_ = home
	return nil
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
		// Already cloned — pull.
		cmd := exec.Command("git", "-C", dir, "pull")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}

	args := []string{"clone", "--depth=1"}
	if ref != "" && ref != "main" && ref != "master" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repoURL, dir)

	cmd := exec.Command("git", args...)
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

	onixExe := filepath.Join(config.Dir(), "onix.exe")
	content := fmt.Sprintf("@echo off\r\nset \"ONIX_MODULE=%s\"\r\n\"%s\" %%*\r\n", name, onixExe)

	return os.WriteFile(filepath.Join(config.BinDir(), name+".cmd"), []byte(content), 0o644)
}

func isInstalled(name string) bool {
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
