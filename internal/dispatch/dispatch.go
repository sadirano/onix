package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
)

// Run resolves the alias, chdirs to the target, and executes the named module
// binary with the remaining args.
//
// Environment variables set for the module:
//
//	ONIX_MODULE        module name
//	ONIX_ALIAS         the alias string the user typed
//	ONIX_TARGET        resolved absolute target path (same as CWD)
//	ONIX_MODULE_CONFIG JSON-encoded module config section
func Run(moduleName, aliasName string, args []string, cfg *config.Config) error {
	debug := cfg.Settings.Debug || os.Getenv("ONIX_DEBUG") == "1" || os.Getenv("OMNI_DEBUG") == "1"

	target, err := alias.Resolve(aliasName, debug)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create target %q: %w", target, err)
	}
	if err := os.Chdir(target); err != nil {
		return fmt.Errorf("chdir to %q: %w", target, err)
	}

	binPath := filepath.Join(config.ModulesDir(), moduleName, moduleName+".exe")
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("module %q not found at %s — run: onix install %s", moduleName, binPath, moduleName)
	}

	mod := cfg.FindModule(moduleName)
	modConfigJSON := "{}"
	if mod != nil {
		modConfigJSON = mod.ConfigJSON()
	}

	if debug {
		fmt.Printf("[ONIX] module=%q alias=%q target=%q args=%v\n", moduleName, aliasName, target, args)
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = target
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(),
		"ONIX_MODULE="+moduleName,
		"ONIX_ALIAS="+aliasName,
		"ONIX_TARGET="+target,
		"ONIX_MODULE_CONFIG="+modConfigJSON,
	)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start module %q: %w", moduleName, err)
	}
	return cmd.Wait()
}
