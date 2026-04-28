package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
)

// coreCommands is the canonical list of built-in onix command names that must
// never be intercepted by module dispatch, even when ONIX_MODULE is set.
var coreCommands = []string{
	"install", "add", "remove", "update", "list", "init", "shortcuts",
	"theme", "themes", "help", "-h", "--help", "-a", "--alias",
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
//	ONIX_ALIAS         the alias string the user typed
//	ONIX_TARGET        resolved absolute target path (same as CWD)
//	ONIX_MODULE_CONFIG JSON-encoded module config section
//	ONIX_HOME          onix home directory (~/.onix)
//	ONIX_EDITOR        resolved editor (from config, then EDITOR env, then nvim)
func Run(moduleName, aliasName string, args []string, cfg *config.Config) error {
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
	return runModule(moduleName, moduleName, aliasName, target, args, cfg)
}

// RunResolved executes the named module with a pre-resolved target directory.
// Used when the caller has already resolved the alias and applied any subdirectory,
// bypassing the alias resolution step inside Run.
//
// binModule overrides the directory/binary name used to locate the executable
// (e.g. "onix-sg" when the action/ONIX_MODULE name is "sg"). Pass "" to use
// moduleName for both.
func RunResolved(moduleName, binModule, target string, args []string, cfg *config.Config) error {
	if binModule == "" {
		binModule = moduleName
	}
	return runModule(moduleName, binModule, "", target, args, cfg)
}

// runModule builds and executes the module command. aliasName is included in
// the environment as ONIX_ALIAS only when non-empty.
func runModule(moduleName, binModule, aliasName, target string, args []string, cfg *config.Config) error {
	debug := cfg.IsDebugEnabled()

	binPath := filepath.Join(config.ModulesDir(), binModule, binModule+".exe")
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("module %q not found at %s — run: onix install %s", binModule, binPath, binModule)
	}

	mod := cfg.FindModule(binModule)
	modConfigJSON := "{}"
	if mod != nil {
		modConfigJSON = mod.ConfigJSON()
	}

	if debug {
		fmt.Printf("[ONIX] module=%q bin=%q alias=%q target=%q args=%v\n", moduleName, binModule, aliasName, target, args)
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

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start module %q: %w", moduleName, err)
	}
	return cmd.Wait()
}
