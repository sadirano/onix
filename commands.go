package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/errs"
	"github.com/sadirano/onix/internal/installer"
)

// handleManagementCommand executes a built-in onix subcommand (install, add,
// remove, …) and returns true. Returns false when args[0] is not a management
// command, so the caller can fall through to alias resolution.
func handleManagementCommand(args []string, cfg *config.Config, t *timer, debugEnabled bool) bool {
	switch args[0] {
	case "install":
		t.mark("config loaded")
		var installModule, installProfile string
		for _, a := range args[1:] {
			if strings.HasPrefix(a, "-") {
				installProfile = strings.TrimPrefix(a, "-")
			} else {
				installModule = a
			}
		}
		if installModule != "" {
			if err := installer.Install(installModule, cfg); err != nil {
				errs.Fatal("%v", err)
			}
		} else {
			if err := installer.InstallAll(cfg); err != nil {
				errs.Fatal("%v", err)
			}
		}
		if err := installer.InstallShortcutsProfile(installProfile, cfg); err != nil {
			errs.Fatal("%v", err)
		}
		t.mark("install")

	case "add":
		switch len(args) {
		case 2:
			if err := installer.Add(args[1], "", cfg); err != nil {
				errs.Fatal("%v", err)
			}
		case 3:
			if err := installer.Add(args[1], args[2], cfg); err != nil {
				errs.Fatal("%v", err)
			}
		default:
			errs.Fatal("usage: onix add <user/repo> [name]")
		}

	case "remove":
		if len(args) < 2 {
			errs.Fatal("usage: onix remove <name>")
		}
		if err := installer.Remove(args[1], cfg); err != nil {
			errs.Fatal("%v", err)
		}

	case "update":
		t.mark("config loaded")
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		if err := installer.Update(name, cfg); err != nil {
			errs.Fatal("%v", err)
		}
		if err := installer.InstallShortcuts(cfg); err != nil {
			errs.Fatal("%v", err)
		}
		t.mark("update")

	case "list":
		installer.List(cfg)

	case "init":
		if err := installer.Init(); err != nil {
			errs.Fatal("%v", err)
		}

	case "-h", "--help", "help":
		printHelp()

	default:
		return false
	}
	return true
}

// registerAlias parses -a/-d/-s flags, writes the alias, and returns the
// resolved destination path. Calls fatal on any missing required flag.
func registerAlias(args []string) string {
	var aliasName, destination, subdir string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-a", "--alias":
			if i+1 < len(args) {
				aliasName = args[i+1]
				i++
			}
		case "-d", "--destination":
			if i+1 < len(args) {
				destination = args[i+1]
				i++
			}
		case "-s", "--subdir":
			if i+1 < len(args) {
				subdir = args[i+1]
				i++
			}
		}
	}

	if subdir != "" {
		destination = filepath.Join(destination, subdir)
	}
	if err := alias.Register(aliasName, destination); err != nil {
		errs.Fatal("register alias: %v", err)
	}
	fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, destination)
	return destination
}

// parseExtras extracts an optional subdir (-s/--subdir) and positional extras
// from the tail of an alias invocation. The action is now carried by the
// ONIX_COMMAND environment variable set by the .cmd wrapper, not by a flag.
func parseExtras(args []string) (subdir string, extras []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "-s" || args[i] == "--subdir" {
			if i+1 < len(args) {
				subdir = args[i+1]
				i++
			}
		} else {
			extras = append(extras, args[i])
		}
	}
	return
}

// parseAllSegments splits "seg1@seg2@alias" into (["seg1","seg2"], "alias").
// The alias is always the last token (after the last @). Segments are returned
// in left-to-right order as written by the user; callers process them
// right-to-left to build the path innermost-first.
// Returns (nil, input) when no "@" is present.
func parseAllSegments(input string) (segments []string, aliasName string) {
	i := strings.LastIndex(input, "@")
	if i < 0 {
		return nil, input
	}
	raw, aliasName := input[:i], input[i+1:]
	if raw == "" {
		return nil, aliasName
	}
	for _, s := range strings.Split(raw, "@") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	return segments, aliasName
}

// resolveBuiltin maps an ONIX_COMMAND value to the builtin identifier used by
// executeAction. Returns "shell" when cmdName is empty (direct onix invocation).
// Calls fatal when cmdName is set but not found in config.
func resolveBuiltin(cmdName string, cfg *config.Config) string {
	if cmdName == "" {
		return "shell"
	}
	action := cfg.FindAction(cmdName)
	if action == nil {
		errs.Fatal("unknown command %q — check [[action]] blocks in config", cmdName)
	}
	return action.Builtin
}

// printHelp writes the usage message to stdout.
func printHelp() {
	fmt.Print(`Onix — modular directory navigator

Usage:
  onix                          open alias file in editor
  onix <alias>                  open shell in target directory
  onix -a <alias> -d <path>     register an alias
  onix install [name] [-profile] install one or all modules; -profile applies a named shortcut set
  onix add <user/repo> [name]   declare a module in config
  onix remove <name>            remove a module
  onix update [name]            update one or all modules
  onix list                     list declared modules
  onix shortcuts                install .cmd wrappers in ~/.onix/bin/
  onix init                     initialise ~/.onix/ structure
  onix help                     show this message

Action invocation (via generated wrappers):
  <action> <alias> [args...]    e.g. editor myproject

Module invocation (via generated wrappers):
  <module> <alias> [args...]    e.g. mymodule myproject foo bar

Environment:
  ONIX_COMMAND       set by action .cmd wrappers
  ONIX_MODULE        set by module .cmd wrappers
  ONIX_DEBUG=1       verbose trace
  ONIX_TIMING=1      print phase timings to stderr
  ONIX_ENV           override alias file path
  EDITOR             preferred editor (default: nvim)

Config:  ~/.onix/config.toml
Modules: ~/.onix/modules/
Bin:     ~/.onix/bin/   ← add this to PATH
`)
}
