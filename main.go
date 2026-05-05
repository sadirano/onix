package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/dispatch"
	"github.com/sadirano/onix/internal/errs"
	"github.com/sadirano/onix/internal/installer"
)

// Onix — a modular directory navigator.
//
// Direct invocation:
//
//	onix                          open alias file in editor
//	onix -a <alias> -d <path>     register an alias
//	onix <alias>                  open cmd.exe in target directory (built-in default)
//	onix install [name]           install one or all modules
//	onix add <user/repo> [name]   declare a new module in config
//	onix remove <name>            remove a module
//	onix update [name]            update one or all modules
//	onix list                     list declared modules
//	onix init                     set up ~/.onix/ directory structure
//
// Action dispatch (via .cmd wrapper that sets ONIX_COMMAND):
//
//	ONIX_COMMAND=editor onix <alias> [args...]
//
// Module dispatch (via .cmd wrapper that sets ONIX_MODULE):
//
//	ONIX_MODULE=mymodule onix <alias> [args...]

// Exit codes used by onix.
const (
	exitOK      = 0
	exitErr     = 1 // general error
	exitUsage   = 2 // bad arguments / usage error
	exitNotFound = 3 // alias or module not found
)

func main() {
	t := newTimer()
	defer t.report()

	cfg, err := config.Load()
	if err != nil {
		fatalCode(exitErr, "load config: %v", err)
	}
	alias.ApplyEnvOverride(cfg.Settings.AliasFile)
	debugEnabled := cfg.IsDebugEnabled()

	if debugEnabled {
		printBuildDebugInfo()
	}

	args := os.Args[1:]

	// Module dispatch — invoked via a .cmd wrapper that sets ONIX_MODULE.
	if mod := strings.TrimSpace(os.Getenv("ONIX_MODULE")); mod != "" && !dispatch.IsCoreCommand(args) {
		t.mark("config loaded")
		if len(args) == 0 {
			fatalCode(exitUsage, "usage: %s <alias> [args...]", mod)
		}
		if err := installer.EnsureInstalled(mod, cfg); err != nil {
			fatalCode(exitNotFound, "%v", err)
		}
		entry := strings.TrimSpace(os.Getenv("ONIX_ENTRY"))
		if err := dispatch.Run(mod, entry, args[0], args[1:], cfg); err != nil {
			handleActionErr(err)
		}
		t.mark("dispatch")
		return
	}

	// Action dispatch — invoked via a .cmd wrapper that sets ONIX_COMMAND.
	cmdName := strings.TrimSpace(os.Getenv("ONIX_COMMAND"))

	// No args — open the alias file in the editor (only when not via a wrapper).
	if len(args) == 0 {
		if cmdName != "" {
			fatalCode(exitUsage, "usage: %s <alias> [args...]", cmdName)
		}
		if err := alias.OpenInEditor(cfg.ResolveEditor()); err != nil {
			fatalCode(exitErr, "%v", err)
		}
		return
	}

	// Management commands: install, add, remove, update, list, init, help.
	if handleManagementCommand(args, cfg, t, debugEnabled) {
		return
	}

	// Alias registration: onix -a <alias> -d <path>
	if args[0] == "-a" || args[0] == "--alias" {
		destination := registerAlias(args)
		if cmdName != "" {
			builtin := resolveBuiltin(cmdName, cfg)
			if !isUNCPath(destination) {
				if err := os.MkdirAll(destination, 0o755); err != nil {
					fatalCode(exitErr, "create target: %v", err)
				}
			}
			t.mark("action after register")
			if err := executeAction(builtin, destination, nil, cfg, t); err != nil {
				handleActionErr(err)
			}
		}
		return
	}

	// Default: resolve alias, walk @ segments, chdir, execute action.
	t.mark("config loaded")
	segments, aliasName := parseAllSegments(args[0])
	subdir, extras := parseExtras(args[1:])

	target, resolveErr := alias.Resolve(aliasName, debugEnabled)
	if resolveErr != nil {
		// UX1: alias not found — show the error and ask the user to provide a destination.
		fmt.Fprintf(os.Stderr, "onix: %v\n", resolveErr)
		dest := promptDestination(aliasName)
		if dest == "" {
			os.Exit(exitNotFound)
		}
		abs, err := filepath.Abs(dest) // C4: resolve to absolute before registering
		if err != nil {
			fatalCode(exitErr, "resolve path: %v", err)
		}
		if err := alias.Register(aliasName, abs); err != nil {
			fatalCode(exitErr, "register alias: %v", err)
		}
		fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, abs)
		target = abs
	}
	t.mark("alias resolved")

	// Walk segments right-to-left (closest to alias first) so that
	// "task@client@place" appends client's contribution before task's.
	for i := len(segments) - 1; i >= 0; i-- {
		part, err := applySegment(segments[i], target, cfg, debugEnabled)
		if err != nil {
			fatalCode(exitErr, "%v", err)
		}
		target = filepath.Join(target, part)
	}

	if subdir != "" {
		target = filepath.Join(target, subdir)
	}
	if !isUNCPath(target) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			fatalCode(exitErr, "create target: %v", err)
		}
		if err := os.Chdir(target); err != nil {
			fatalCode(exitErr, "chdir: %v", err)
		}
	}
	t.mark("chdir")

	builtin := resolveBuiltin(cmdName, cfg)
	if err := executeAction(builtin, target, extras, cfg, t); err != nil {
		handleActionErr(err)
	}
}

// handleActionErr checks whether err is an ExitError (child process exit code)
// and exits with that code, otherwise fatals with exitErr.
func handleActionErr(err error) {
	if ee, ok := err.(*errs.ExitError); ok {
		os.Exit(ee.Code)
	}
	fatalCode(exitErr, "%v", err)
}
