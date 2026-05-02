package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/installer"
)

// handleManagementCommand executes a built-in onix subcommand (install, add,
// remove, …) and returns true. Returns false when args[0] is not a management
// command, so the caller can fall through to alias resolution.
func handleManagementCommand(args []string, cfg *config.Config, t *timer, debugEnabled bool) bool {
	switch args[0] {
	case "install":
		t.mark("config loaded")
		if len(args) > 1 {
			if err := installer.Install(args[1], cfg); err != nil {
				fatal("%v", err)
			}
		} else {
			if err := installer.InstallAll(cfg); err != nil {
				fatal("%v", err)
			}
		}
		t.mark("install")

	case "add":
		switch len(args) {
		case 2:
			if err := installer.Add(args[1], "", cfg); err != nil {
				fatal("%v", err)
			}
		case 3:
			if err := installer.Add(args[2], args[1], cfg); err != nil {
				fatal("%v", err)
			}
		default:
			fatal("usage: onix add [<name>] <user/repo>")
		}

	case "remove":
		if len(args) < 2 {
			fatal("usage: onix remove <name>")
		}
		if err := installer.Remove(args[1], cfg); err != nil {
			fatal("%v", err)
		}

	case "update":
		t.mark("config loaded")
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		if err := installer.Update(name, cfg); err != nil {
			fatal("%v", err)
		}
		t.mark("update")

	case "list":
		installer.List(cfg)

	case "init":
		if err := installer.Init(); err != nil {
			fatal("%v", err)
		}

	case "shortcuts":
		if err := installer.InstallShortcuts(); err != nil {
			fatal("%v", err)
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

	if aliasName == "" || destination == "" {
		fatal("usage: onix -a <alias> -d <destination>")
	}
	if subdir != "" {
		destination = filepath.Join(destination, subdir)
	}
	if err := alias.Register(aliasName, destination); err != nil {
		fatal("register alias: %v", err)
	}
	fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, destination)
	return destination
}

// actionFlags maps CLI flag strings to their action names.
var actionFlags = map[string]string{
	"-e": "e", "-n": "n", "-y": "y", "-f": "f", "-r": "r",
}

// parseActionArgs extracts the action flag, optional subdir, and positional
// extras from the tail of an alias invocation.
// .cmd wrappers append the action flag last (e.g. `o %* -n`), so positional
// extras appear before the flag.
func parseActionArgs(args []string) (action, subdir string, extras []string) {
	for i := 0; i < len(args); i++ {
		if a, ok := actionFlags[args[i]]; ok {
			action = a
		} else if args[i] == "-s" || args[i] == "--subdir" {
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

// printHelp writes the usage message to stdout.
func printHelp() {
	fmt.Print(`Onix — modular directory navigator

Usage:
  onix                          open alias file in editor
  onix <alias>                  open shell in target directory
  onix -a <alias> -d <path>     register an alias
  onix install [name]           install one or all modules
  onix add <user/repo>          declare a module in config
  onix remove <name>            remove a module
  onix update [name]            update one or all modules
  onix list                     list declared modules
  onix init                     initialise ~/.onix/ structure
  onix help                     show this message

Module invocation (via generated wrappers):
  <module> <alias> [args...]    e.g. mymodule myproject foo bar

Environment:
  ONIX_MODULE        set by .cmd wrappers to select the module
  ONIX_DEBUG=1       verbose trace
  ONIX_TIMING=1      print phase timings to stderr
  ONIX_ENV           override alias file path
  EDITOR             preferred editor (default: nvim)

Config:  ~/.onix/config.toml
Modules: ~/.onix/modules/
Bin:     ~/.onix/bin/   ← add this to PATH
`)
}

// fatal prints an error to stderr and exits with code 1.
func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "onix: "+format+"\n", a...)
	os.Exit(1)
}
