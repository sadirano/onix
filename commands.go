package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		if err := installer.InstallShortcuts(cfg); err != nil {
			fatal("%v", err)
		}
		t.mark("install")

	case "add":
		switch len(args) {
		case 2:
			if err := installer.Add(args[1], "", cfg); err != nil {
				fatal("%v", err)
			}
		case 3:
			if err := installer.Add(args[1], args[2], cfg); err != nil {
				fatal("%v", err)
			}
		default:
			fatal("usage: onix add <user/repo> [name]")
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
		if err := installer.InstallShortcuts(cfg); err != nil {
			fatal("%v", err)
		}
		t.mark("update")

	case "list":
		installer.List(cfg)

	case "init":
		if err := installer.Init(); err != nil {
			fatal("%v", err)
		}

	case "ctx":
		if len(args) < 2 {
			fatal("usage: onix ctx <alias> [env <var> | cmd <command> | file <path> | --clear]")
		}
		a := args[1]
		switch {
		case len(args) == 2:
			printAliasContextConfig(a)
		case args[2] == "--clear":
			if err := clearAliasContext(a); err != nil {
				fatal("clear context: %v", err)
			}
			fmt.Printf("Context for %q cleared\n", a)
		case args[2] == "env":
			if len(args) < 4 {
				fatal("usage: onix ctx <alias> env <var>")
			}
			cc := config.ContextConfig{Source: "env", Var: args[3]}
			if err := writeAliasContextConfig(a, cc); err != nil {
				fatal("write context: %v", err)
			}
			fmt.Printf("Context for %q: source=env var=%s\n", a, args[3])
		case args[2] == "cmd":
			if len(args) < 4 {
				fatal("usage: onix ctx <alias> cmd <command>")
			}
			cc := config.ContextConfig{Source: "cmd", Cmd: strings.Join(args[3:], " ")}
			if err := writeAliasContextConfig(a, cc); err != nil {
				fatal("write context: %v", err)
			}
			fmt.Printf("Context for %q: source=cmd cmd=%s\n", a, cc.Cmd)
		case args[2] == "file":
			if len(args) < 4 {
				fatal("usage: onix ctx <alias> file <path>")
			}
			cc := config.ContextConfig{Source: "file", File: args[3]}
			if err := writeAliasContextConfig(a, cc); err != nil {
				fatal("write context: %v", err)
			}
			fmt.Printf("Context for %q: source=file file=%s\n", a, args[3])
		default:
			fatal("unknown context source %q — use env, cmd, or file", args[2])
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

// parseSubAlias splits "sub@alias" into ("sub", "alias").
// Returns ("", input) when no "@" is present — plain alias invocation unchanged.
//
// Edge cases:
//
//	"@alias"  → subAlias="",    aliasName="alias"  (treated as plain alias)
//	"sub@"    → subAlias="sub", aliasName=""        (caller must handle empty alias)
//	"a@b@c"   → subAlias="a",   aliasName="b@c"    (first @ is the separator)
func parseSubAlias(input string) (subAlias, aliasName string) {
	i := strings.IndexByte(input, '@')
	if i < 0 {
		return "", input
	}
	return input[:i], input[i+1:]
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
		fatal("unknown command %q — check [[action]] blocks in config", cmdName)
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
  onix install [name]           install one or all modules
  onix add <user/repo> [name]   declare a module in config
  onix remove <name>            remove a module
  onix update [name]            update one or all modules
  onix list                     list declared modules
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

// fatal prints an error to stderr and exits with code 1 (general error).
func fatal(format string, a ...any) {
	fatalCode(exitErr, format, a...)
}

// fatalCode prints an error to stderr and exits with the given code.
func fatalCode(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "onix: "+format+"\n", a...)
	os.Exit(code)
}
