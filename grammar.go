package main

// Grammar implements the alias-flag CLI shape:
//
//   onix [<alias>] [<path>] [--<action>] [--<flag>=value ...] [args...]
//
// The alias is the subject (first positional); the verb is a flag. Commands
// with no alias operate on the alias system as a whole; with an alias, the
// flag acts on that alias.
//
// This dispatcher runs BEFORE kong. If the argv matches the new grammar we
// route directly to the existing command structs. If not (i.e. the user
// typed an old subcommand like `onix resolve foo` or `onix plugin add ...`)
// we return handled=false and main.go falls through to the kong path. This
// keeps installed shell snippets working until they re-sync.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// atoi is a thin wrapper that gives the dispatcher a uniform error message
// for flags taking integer values (--top, future --head, --tail).
func atoi(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("expected integer, got %q", s)
	}
	return n, nil
}

// Multi-char short flags that aren't expressible as kong single-rune shorts.
// These are rewritten to their long forms before any other parsing.
var multiCharShortRewrite = map[string]string{
	"-ls": "--list",
	"-rm": "--remove",
}

// preprocessArgs rewrites multi-char short flags (-ls, -rm) into their long
// forms. Single-rune shorts (-l, -e, ...) and flags with values (-d "x") are
// left alone — kong handles those natively.
func preprocessArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if rewrite, ok := multiCharShortRewrite[a]; ok {
			out[i] = rewrite
		} else {
			out[i] = a
		}
	}
	return out
}

// aliasActionFlags lists every flag that turns "onix <alias> ..." into an
// action invocation. Long form -> canonical action name. Single-rune shorts
// are listed alongside their long form.
var aliasActionFlags = map[string]string{
	"--resolve": "resolve",
	"--remove":  "remove",
	"--rm":      "remove",
	"--edit":    "edit",
	"-e":        "edit",
	"--show":    "show",
	"-s":        "show",
	"--explore": "explore",
	"-x":        "explore",
	"--yank":    "yank",
	"-y":        "yank",
	"--grep":    "grep",
	"-g":        "grep",
	"--find":    "find",
	"-f":        "find",
	"--run":     "run",
	"-r":        "run",
	"--exec":    "exec",
	"-X":        "exec",
	"--plugin":  "plugin",
	"-p":        "plugin",
}

// systemActionFlags lists flags that operate on the alias system as a whole
// (no alias positional required). Same long/short convention.
var systemActionFlags = map[string]string{
	"--list":          "list",
	"--ls":            "list",
	"-l":              "list",
	"--list-names":    "list-names",
	"--edit":          "edit",
	"-e":              "edit",
	"--show":          "show",
	"-s":              "show",
	"--contexts":      "contexts",
	"-c":              "contexts",
	"--init":          "init",
	"-I":              "init",
	"--sync":          "sync",
	"-S":              "sync",
	"--doctor":        "doctor",
	"-D":              "doctor",
	"--stats":         "stats",
	"-T":              "stats",
	"--version":       "version",
	"-v":              "version",
	"--apply-context": "apply-context",
}

// legacySubcommands lists every kong subcommand name the binary still
// understands. When the user types one of these as the first positional we
// hand the whole invocation to kong (backwards compat). Anything else is
// treated as an alias.
var legacySubcommands = map[string]bool{
	"resolve":     true,
	"add":         true,
	"remove":      true,
	"rm":          true,
	"list":        true,
	"ls":          true,
	"aliases":     true,
	"edit":        true,
	"grep":        true,
	"find":        true,
	"explore":     true,
	"yank":        true,
	"run":         true,
	"exec":        true,
	"plugin":      true,
	"plugin-exec": true,
	"context":     true,
	"init":        true,
	"sync":        true,
	"list-names":  true,
	"doctor":      true,
	"stats":       true,
	"version":     true,
}

// tryDispatchNewGrammar attempts to handle argv under the new alias-flag
// grammar. Returns (true, err) if the dispatcher took ownership of the call.
// Returns (false, nil) when the call should fall through to kong (legacy
// subcommand form or --help).
func tryDispatchNewGrammar(ctx context.Context, e *env, args []string) (bool, error) {
	if len(args) == 0 {
		// Bare `onix` — let kong print its usage. Fall through.
		return false, nil
	}

	first := args[0]

	// --help / -h: let kong handle it so the user sees consistent help.
	if first == "--help" || first == "-h" {
		return false, nil
	}

	// Plugin management is the one real subcommand that survives the
	// rework. Hand the whole invocation to kong.
	if first == "plugin" {
		return false, nil
	}

	// System-wide flag form: `onix --<verb> [args...]`.
	if startsWithDash(first) {
		if verb, ok := systemActionFlags[first]; ok {
			return true, dispatchSystem(ctx, e, verb, args[1:])
		}
		// Unknown flag at top level — let kong report it.
		return false, nil
	}

	// First positional is a legacy subcommand: hand to kong for backwards compat.
	if legacySubcommands[first] {
		return false, nil
	}

	// Alias-flag form: first positional is the alias name.
	return true, dispatchAlias(ctx, e, first, args[1:])
}

// dispatchSystem handles `onix --<verb> [args...]`.
func dispatchSystem(ctx context.Context, e *env, verb string, rest []string) error {
	switch verb {
	case "list":
		return (&ListCmd{}).Run(ctx, e)
	case "list-names":
		// Hot path — go through fastListNames directly so behaviour matches
		// the existing `onix list-names` invocation byte-for-byte.
		return fastListNames(e.Home)
	case "edit":
		// System-wide --edit currently maps to AliasesCmd (opens
		// aliases.toml). File-arg passthrough lands in a follow-up commit.
		return (&AliasesCmd{}).Run(ctx, e)
	case "show":
		return (&ShowCmd{Args: rest}).Run(ctx, e)
	case "contexts":
		return (&ContextListCmd{}).Run(ctx, e)
	case "init":
		return (&InitCmd{}).Run(ctx, e)
	case "sync":
		return (&SyncCmd{}).Run(ctx, e)
	case "doctor":
		return (&DoctorCmd{}).Run(ctx, e)
	case "stats":
		return runStatsFromArgs(ctx, e, rest)
	case "version":
		return (&VersionCmd{}).Run(ctx, e)
	case "apply-context":
		if len(rest) == 0 {
			return fmt.Errorf("--apply-context requires an alias name")
		}
		shell := "pwsh"
		alias := ""
		for i := 0; i < len(rest); i++ {
			a := rest[i]
			if a == "--shell" && i+1 < len(rest) {
				shell = rest[i+1]
				i++
				continue
			}
			if strings.HasPrefix(a, "--shell=") {
				shell = strings.TrimPrefix(a, "--shell=")
				continue
			}
			if alias == "" && !startsWithDash(a) {
				alias = a
				continue
			}
		}
		if alias == "" {
			return fmt.Errorf("--apply-context requires an alias name")
		}
		return applyContexts(e.Home, alias, shell, os.Stdout)
	}
	return fmt.Errorf("unknown system action %q", verb)
}

// dispatchAlias handles `onix <alias> [<path>|--action ...]`.
func dispatchAlias(ctx context.Context, e *env, alias string, rest []string) error {
	// Find the first action flag, if any. Everything before it is the
	// add-form (path + metadata flags); everything after is the action's
	// args (passthrough).
	actionIdx := -1
	action := ""
	for i, a := range rest {
		if v, ok := aliasActionFlags[a]; ok {
			actionIdx = i
			action = v
			break
		}
	}

	if actionIdx == -1 {
		// No action flag: either a bare resolve or an add-form invocation.
		return dispatchAliasAddOrResolve(ctx, e, alias, rest)
	}

	preAction := rest[:actionIdx]
	actionArgs := rest[actionIdx+1:]

	// Pre-action args (between the alias and the action flag) only make
	// sense for add+action combos, which we don't support. Reject them
	// explicitly so typos like `onix foo extra --edit` don't silently drop
	// the "extra" token.
	if len(preAction) > 0 {
		return fmt.Errorf("unexpected positional %q before --%s", preAction[0], action)
	}

	switch action {
	case "resolve":
		return fastResolve(e.Home, alias, false)
	case "remove":
		// Today: remove the alias. File-delete support lands in a follow-up.
		return (&RemoveCmd{Alias: alias}).Run(ctx, e)
	case "edit":
		// File-arg passthrough lands in a follow-up; for now ignore extras.
		_ = actionArgs
		return (&EditCmd{Alias: alias}).Run(ctx, e)
	case "show":
		return (&ShowCmd{Alias: alias, Args: actionArgs}).Run(ctx, e)
	case "explore":
		return (&ExploreCmd{Alias: alias}).Run(ctx, e)
	case "yank":
		return (&YankCmd{Alias: alias}).Run(ctx, e)
	case "grep":
		return (&GrepCmd{Args: append([]string{alias}, actionArgs...)}).Run(ctx, e)
	case "find":
		return (&FindCmd{Args: append([]string{alias}, actionArgs...)}).Run(ctx, e)
	case "run":
		return (&RunCmd{Args: append([]string{alias}, actionArgs...)}).Run(ctx, e)
	case "exec":
		if len(actionArgs) == 0 {
			return fmt.Errorf("--exec requires an action name")
		}
		// ExecCmd's argv shape is [actionName, alias, extras...].
		return (&ExecCmd{Args: append([]string{actionArgs[0], alias}, actionArgs[1:]...)}).Run(ctx, e)
	case "plugin":
		if len(actionArgs) == 0 {
			return fmt.Errorf("--plugin requires a plugin name")
		}
		// PluginExecCmd's argv shape is [pluginName, entryName, alias, extras...].
		// The new grammar doesn't expose entry selection yet; pass "" as entry.
		return (&PluginExecCmd{Args: append([]string{actionArgs[0], "", alias}, actionArgs[1:]...)}).Run(ctx, e)
	}
	return fmt.Errorf("unknown action %q", action)
}

// dispatchAliasAddOrResolve handles `onix <alias>` and `onix <alias> <path> [metadata...]`.
func dispatchAliasAddOrResolve(ctx context.Context, e *env, alias string, rest []string) error {
	// Strip out --no-prompt so it can appear in either position.
	noPrompt := false
	cleaned := make([]string, 0, len(rest))
	for _, a := range rest {
		if a == "--no-prompt" || a == "-q" {
			noPrompt = true
			continue
		}
		cleaned = append(cleaned, a)
	}

	if len(cleaned) == 0 {
		// Bare `onix <alias>` — hot-path resolve.
		return fastResolve(e.Home, alias, noPrompt)
	}

	// Parse: <path> [--description X] [--owner X] [--tags X]...
	add := &AddCmd{Alias: alias}
	for i := 0; i < len(cleaned); i++ {
		a := cleaned[i]
		switch {
		case a == "--description" || a == "-d":
			if i+1 >= len(cleaned) {
				return fmt.Errorf("--description requires a value")
			}
			add.Description = cleaned[i+1]
			i++
		case strings.HasPrefix(a, "--description="):
			add.Description = strings.TrimPrefix(a, "--description=")
		case a == "--owner" || a == "-o":
			if i+1 >= len(cleaned) {
				return fmt.Errorf("--owner requires a value")
			}
			add.Owner = cleaned[i+1]
			i++
		case strings.HasPrefix(a, "--owner="):
			add.Owner = strings.TrimPrefix(a, "--owner=")
		case a == "--tags" || a == "-t":
			if i+1 >= len(cleaned) {
				return fmt.Errorf("--tags requires a value")
			}
			add.Tags = append(add.Tags, cleaned[i+1])
			i++
		case strings.HasPrefix(a, "--tags="):
			add.Tags = append(add.Tags, strings.TrimPrefix(a, "--tags="))
		case startsWithDash(a):
			return fmt.Errorf("unknown flag %q on add form", a)
		default:
			if add.Path != "" {
				return fmt.Errorf("unexpected positional %q (path already set to %q)", a, add.Path)
			}
			add.Path = a
		}
	}
	if add.Path == "" {
		// Only metadata flags, no path — fall back to resolve (with prompt
		// suppression already handled above).
		return fastResolve(e.Home, alias, noPrompt)
	}
	return add.Run(ctx, e)
}

// runStatsFromArgs parses `--stats [--full] [--cold] [--since 30d]` and runs
// StatsCmd. Stats flags are forwarded literally so the surface matches the
// old `onix stats` invocation.
func runStatsFromArgs(ctx context.Context, e *env, args []string) error {
	cmd := &StatsCmd{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--full":
			cmd.Full = true
		case a == "--cold":
			cmd.Cold = true
		case a == "--since":
			if i+1 >= len(args) {
				return fmt.Errorf("--since requires a value")
			}
			cmd.Since = args[i+1]
			i++
		case strings.HasPrefix(a, "--since="):
			cmd.Since = strings.TrimPrefix(a, "--since=")
		case a == "--top":
			if i+1 >= len(args) {
				return fmt.Errorf("--top requires a value")
			}
			n, err := atoi(args[i+1])
			if err != nil {
				return err
			}
			cmd.Top = n
			i++
		case strings.HasPrefix(a, "--top="):
			n, err := atoi(strings.TrimPrefix(a, "--top="))
			if err != nil {
				return err
			}
			cmd.Top = n
		default:
			return fmt.Errorf("unknown stats flag %q", a)
		}
	}
	return cmd.Run(ctx, e)
}
