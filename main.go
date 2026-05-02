package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/dispatch"
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
//	onix add <user/repo>          declare a new module in config
//	onix remove <name>            remove a module
//	onix update [name]            update one or all modules
//	onix list                     list declared modules
//	onix init                     set up ~/.onix/ directory structure
//
// Module dispatch (via wrapper):
//
//	ONIX_MODULE=mymodule onix <alias> [args...]

// shortcuts maps executable basenames to their implicit action flag.
// Executables in ~/.onix/bin/ are copies of onix.exe named after each
// shortcut; the binary detects its own name and injects the flag.
var shortcuts = config.Shortcuts

func main() {
	invokedAs := execBasename()

	// Named-shortcut dispatch: inject the action flag when invoked as s.exe, n.exe, etc.
	if flag, ok := shortcuts[invokedAs]; ok && flag != "" {
		os.Args = append(os.Args, flag)
	}

	t := newTimer()
	defer t.report()

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
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
			fatal("usage: %s <alias> [args...]", mod)
		}
		if err := installer.EnsureInstalled(mod, cfg); err != nil {
			fatal("%v", err)
		}
		entry := strings.TrimSpace(os.Getenv("ONIX_ENTRY"))
		if err := dispatch.Run(mod, entry, args[0], args[1:], cfg); err != nil {
			fatal("%v", err)
		}
		t.mark("dispatch")
		return
	}

	// No args — open the alias file in the editor.
	if len(args) == 0 {
		if err := alias.OpenInEditor(cfg.ResolveEditor()); err != nil {
			fatal("%v", err)
		}
		return
	}

	// Management commands: install, add, remove, update, list, init, shortcuts, theme, help.
	if handleManagementCommand(args, cfg, t, debugEnabled) {
		return
	}

	// Alias registration: onix -a <alias> -d <path>
	if args[0] == "-a" || args[0] == "--alias" {
		destination := registerAlias(args)
		if invokedAs == "o" || invokedAs == "c" {
			if !isUNCPath(destination) {
				if err := os.MkdirAll(destination, 0o755); err != nil {
					fatal("create target: %v", err)
				}
			}
			t.mark("shell spawned")
			if err := openShellAt(destination); err != nil {
				fatal("open shell: %v", err)
			}
		}
		return
	}

	// Default: resolve alias, apply subdir, chdir, execute action.
	t.mark("config loaded")
	aliasName := args[0]
	action, subdir, extras := parseActionArgs(args[1:])

	target, err := alias.Resolve(aliasName, debugEnabled)
	if err != nil {
		dest := promptDestination(aliasName)
		if dest == "" {
			fatal("no destination provided")
		}
		if err := alias.Register(aliasName, dest); err != nil {
			fatal("register alias: %v", err)
		}
		fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, dest)
		target = dest
	}
	t.mark("alias resolved")

	if subdir != "" {
		target = filepath.Join(target, subdir)
	}
	if !isUNCPath(target) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			fatal("create target: %v", err)
		}
		if err := os.Chdir(target); err != nil {
			fatal("chdir: %v", err)
		}
	}
	t.mark("chdir")

	executeAction(action, target, extras, cfg, t)
}
