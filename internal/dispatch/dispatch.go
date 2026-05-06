package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/errs"
)

// coreCommands is the canonical list of built-in onix command names that must
// never be intercepted by module dispatch, even when ONIX_MODULE is set.
var coreCommands = []string{
	"install", "add", "remove", "update", "list", "init",
	"help", "-h", "--help", "-a", "--alias",
}

// IsCoreCommand reports whether args[0] is a built-in management command.
func IsCoreCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	first := strings.ToLower(strings.TrimSpace(args[0]))
	for _, c := range coreCommands {
		if first == c {
			return true
		}
	}
	return false
}

// Run resolves the alias, chdirs to the target, and executes the named module
// binary with the remaining args.
//
// Environment variables set for the module:
//
//	ONIX_MODULE        module name
//	ONIX_ENTRY         entry point name (empty for single-entry modules)
//	ONIX_ALIAS         the alias string the user typed
//	ONIX_TARGET        resolved absolute target path (same as CWD)
//	ONIX_MODULE_CONFIG JSON-encoded module config section
//	ONIX_HOME          onix home directory (~/.onix)
//	ONIX_EDITOR        resolved editor (from config, then EDITOR env, then nvim)
func Run(moduleName, entryName, aliasName string, args []string, cfg *config.Config) error {
	target, err := alias.Resolve(aliasName, cfg.IsDebugEnabled())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create target %q: %w", target, err)
	}
	if err := os.Chdir(target); err != nil {
		return fmt.Errorf("chdir to %q: %w", target, err)
	}
	return runModule(moduleName, entryName, aliasName, target, args, cfg)
}

// runResolved executes the named module with a pre-resolved target directory.
func runResolved(moduleName, entryName, target string, args []string, cfg *config.Config) error {
	return runModule(moduleName, entryName, "", target, args, cfg)
}

// RunAtTarget chdir to a pre-resolved target and executes the named module.
// Use this when the caller has already resolved @ segments and knows the final
// target path. aliasName is the base alias (without segment prefixes) set as
// ONIX_ALIAS for the module process; pass "" to omit it.
func RunAtTarget(moduleName, entryName, aliasName, target string, args []string, cfg *config.Config) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create target %q: %w", target, err)
	}
	if err := os.Chdir(target); err != nil {
		return fmt.Errorf("chdir to %q: %w", target, err)
	}
	return runModule(moduleName, entryName, aliasName, target, args, cfg)
}

// runModule builds and executes the module command. aliasName is included in
// the environment as ONIX_ALIAS only when non-empty.
func runModule(moduleName, entryName, aliasName, target string, args []string, cfg *config.Config) error {
	debug := cfg.IsDebugEnabled()

	mod := cfg.FindModule(moduleName)
	if mod == nil {
		return fmt.Errorf("module %q not found in config — run: onix add <repo>", moduleName)
	}

	srcDir := config.ModuleDir(mod.Repo)
	binName := config.RepoBinName(mod.Repo) + ".exe"
	binPath := filepath.Join(srcDir, binName)
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("module %q not found at %s — run: onix install %s", moduleName, binPath, moduleName)
	}

	modConfigJSON := mod.ConfigJSON()

	// For multi-entry modules, prepend the entry name to args.
	if entryName != "" {
		args = append([]string{entryName}, args...)
	}

	if debug {
		fmt.Printf("[ONIX] module=%q entry=%q alias=%q target=%q args=%v\n", moduleName, entryName, aliasName, target, args)
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = target
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(),
		"ONIX_MODULE="+moduleName,
		"ONIX_TARGET="+target,
		"ONIX_MODULE_CONFIG="+modConfigJSON,
		"ONIX_HOME="+config.Dir(),
		"ONIX_EDITOR="+cfg.ResolveEditor(),
	)
	if aliasName != "" {
		cmd.Env = append(cmd.Env, "ONIX_ALIAS="+aliasName)
	}
	if entryName != "" {
		cmd.Env = append(cmd.Env, "ONIX_ENTRY="+entryName)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start module %q: %w", moduleName, err)
	}
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &errs.ExitError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("module %q: %w", moduleName, err)
	}
	return nil
}
