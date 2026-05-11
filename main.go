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
//	onix                          open aliases directory in editor
//	onix -a <alias> -d <path>     register an alias
//	onix <alias>                  open cmd.exe in target directory (built-in default)
//	onix install [name]           install one or all modules
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

func main() {
	t := newTimer()
	defer t.report()

	cfg, err := config.Load()
	if err != nil {
		errs.FatalCode(errs.ExitErr, "load config: %v", err)
	}
	alias.ApplyEnvOverride(cfg.Settings.AliasDir)
	debugEnabled := cfg.IsDebugEnabled()

	if debugEnabled {
		printBuildDebugInfo()
	}

	args := os.Args[1:]

	// Module dispatch — invoked via a .cmd wrapper that sets ONIX_MODULE.
	if mod := strings.TrimSpace(os.Getenv("ONIX_MODULE")); mod != "" && !dispatch.IsCoreCommand(args) {
		t.mark("config loaded")
		if len(args) == 0 {
			errs.FatalCode(errs.ExitUsage, "usage: %s <alias> [args...]", mod)
		}
		if err := installer.EnsureInstalled(mod, cfg); err != nil {
			errs.FatalCode(errs.ExitNotFound, "%v", err)
		}
		entry := strings.TrimSpace(os.Getenv("ONIX_ENTRY"))
		segments, aliasName := parseAllSegments(args[0])
		if len(segments) == 0 {
			// Fast path: no @ segments — delegate full resolve to dispatch.Run.
			if err := dispatch.Run(mod, entry, aliasName, args[1:], cfg); err != nil {
				handleActionErr(err)
			}
		} else {
			target, err := alias.Resolve(aliasName, debugEnabled)
			if err != nil {
				errs.FatalCode(errs.ExitNotFound, "%v", err)
			}
			target = walkSegments(segments, aliasName, target, cfg, debugEnabled)
			if err := dispatch.RunAtTarget(mod, entry, aliasName, target, args[1:], cfg); err != nil {
				handleActionErr(err)
			}
		}
		t.mark("dispatch")
		return
	}

	// Action dispatch — invoked via a .cmd wrapper that sets ONIX_COMMAND.
	cmdName := strings.TrimSpace(os.Getenv("ONIX_COMMAND"))

	// No args — open the alias file.
	if len(args) == 0 {
		if err := alias.OpenInEditor(cfg.ResolveEditor()); err != nil {
			errs.FatalCode(errs.ExitErr, "%v", err)
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
			act := resolveAction(cmdName, cfg)
			if !isUNCPath(destination) {
				if err := os.MkdirAll(destination, 0o755); err != nil {
					errs.FatalCode(errs.ExitErr, "create target: %v", err)
				}
			}
			t.mark("action after register")
			if err := executeAction(act, destination, nil, cfg, t); err != nil {
				handleActionErr(err)
			}
		}
		return
	}

	// Default: resolve alias, walk @ segments, chdir, execute action.
	t.mark("config loaded")
	segments, aliasName := parseAllSegments(args[0])
	subdir, extras := parseExtras(args[1:])

	// Context management: <action> <seg>@<alias> ctx [source <val> [tmpl] | --clear]
	if len(extras) > 0 && extras[0] == "ctx" {
		if len(segments) == 0 {
			errs.FatalCode(errs.ExitUsage, "usage: <action> <segment>@<alias> ctx [env/cmd/file/alias <value>] | [--clear]")
		}
		handleCtxCommand(segments[0]+"@"+aliasName, segments[0], extras[1:], cfg)
		return
	}

	target, resolveErr := alias.Resolve(aliasName, debugEnabled)
	if resolveErr != nil {
		// UX1: alias not found — show the error and ask the user to provide a destination.
		fmt.Fprintf(os.Stderr, "onix: %v\n", resolveErr)
		dest := promptDestination(aliasName)
		if dest == "" {
			os.Exit(errs.ExitNotFound)
		}
		abs, err := filepath.Abs(dest)
		if err != nil {
			errs.FatalCode(errs.ExitErr, "resolve path: %v", err)
		}
		if err := alias.Register(aliasName, abs); err != nil {
			errs.FatalCode(errs.ExitErr, "register alias: %v", err)
		}
		fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, abs)
		target = abs
	}
	t.mark("alias resolved")

	// Walk segments right-to-left (closest to alias first) so that
	// "task@client@place" appends client's contribution before task's.
	target = walkSegments(segments, aliasName, target, cfg, debugEnabled)

	if subdir != "" {
		target = filepath.Join(target, subdir)
	}
	if !isUNCPath(target) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			errs.FatalCode(errs.ExitErr, "create target: %v", err)
		}
		if err := os.Chdir(target); err != nil {
			errs.FatalCode(errs.ExitErr, "chdir: %v", err)
		}
	}
	t.mark("chdir")

	act := resolveAction(cmdName, cfg)
	if err := executeAction(act, target, extras, cfg, t); err != nil {
		handleActionErr(err)
	}
}

// handleActionErr checks whether err is an ExitError (child process exit code)
// and exits with that code, otherwise fatals with ExitErr.
func handleActionErr(err error) {
	if ee, ok := err.(*errs.ExitError); ok {
		os.Exit(ee.Code)
	}
	errs.FatalCode(errs.ExitErr, "%v", err)
}
