package main

// Grammar implements the alias-flag CLI shape:
//
//   onix [<alias>] [<path>] [--<action>] [--<flag>=value ...] [args...]
//
// The alias is the subject (first positional); the verb is a flag. Commands
// with no alias operate on the alias system as a whole; with an alias, the
// flag acts on that alias. The `onix plugin ...` subcommand is the only
// invocation that doesn't fit this shape — main.go routes it to kong
// before calling the dispatcher.

import (
	"context"
	"fmt"
	"io"
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
	"--remove":        "remove",
	"--rm":            "remove",
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
}

// printUsage writes the alias-flag grammar reference to stdout. It's
// hand-rolled because kong's auto-generated help only covers the `plugin`
// subtree — the alias-flag dispatcher owns everything else.
func printUsage(w io.Writer) {
	const usage = `onix — fast directory alias resolver

USAGE:
  onix <alias>                       resolve to absolute path (hot path)
  onix <alias> <path> [--description X] [--owner X] [--tags X]
                                     register or update an alias
  onix <alias> --<action> [args...]  run an action against an alias
  onix --<verb> [args...]            system-wide command
  onix plugin <verb> ...             manage external plugins

ALIAS ACTIONS:
  --resolve              print path (default for bare alias)
  --remove, -rm          remove the alias (or, with files, delete them)
  --edit, -e [files]     open dir or files in $EDITOR
  --show, -s [files]     display dir/file contents (Get-Content / cat)
  --explore, -x          open in OS file manager
  --yank, -y             print path and copy to clipboard
  --grep, -g <query>     ripgrep + fzf in alias dir
  --find, -f <query>     fd / Everything + fzf in alias dir
  --run, -r <cmd...>     exec command in alias dir
  --exec, -X <name>      run a config.toml action
  --plugin, -p <name>    run a plugin (use name:entry for entry-aware plugins)

SYSTEM VERBS:
  --list, -ls, -l        list aliases
  --list-names           one alias name per line (for tab completion)
  --edit, -e [files]     open ~/.onix or files within
  --show, -s [files]     display ~/.onix or files within
  --remove, --rm [files] delete files in ~/.onix (use --force on load-bearing)
  --contexts, -c         list segment contexts
  --init, -I             create ~/.onix and install shell integration
  --sync, -S             regenerate shell snippets
  --doctor, -D           health checks
  --stats, -T [...]      navigation report
  --version, -v          print version

ADD FLAGS:
  --description, -d <text>
  --owner, -o <name>
  --tags, -t <tag>           (repeatable)

REMOVE FLAGS:
  --force, -F                skip confirm; bypass load-bearing file guard
  --recursive, -R            delete directories recursively

GLOBAL:
  --config-dir   ($ONIX_HOME) override ~/.onix path
  --json, -j                 machine-readable output
  --no-prompt, -q            suppress destination prompts on unknown aliases
`
	fmt.Fprint(w, usage)
}

// dispatchNewGrammar parses argv under the alias-flag grammar and runs
// the matching handler. Bare `onix` prints usage; --help is handled here
// so kong (which only owns `plugin`) doesn't get involved.
func dispatchNewGrammar(ctx context.Context, e *env, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	first := args[0]

	if first == "--help" || first == "-h" {
		printUsage(stdout)
		return nil
	}

	// System-wide flag form: `onix --<verb> [args...]`.
	if startsWithDash(first) {
		verb, ok := systemActionFlags[first]
		if !ok {
			return fmt.Errorf("unknown flag %q (run `onix --help` for usage)", first)
		}
		return dispatchSystem(ctx, e, verb, args[1:], stdout, stderr)
	}

	// Alias-flag form: first positional is the alias name.
	return dispatchAlias(ctx, e, first, args[1:])
}

// dispatchSystem handles `onix --<verb> [args...]`.
func dispatchSystem(ctx context.Context, e *env, verb string, rest []string, stdout, stderr io.Writer) error {
	switch verb {
	case "list":
		return (&ListCmd{}).Run(ctx, e)
	case "list-names":
		// Hot path — go through fastListNames directly so behaviour matches
		// the existing `onix list-names` invocation byte-for-byte.
		return fastListNames(e.Home, stdout)
	case "edit":
		// System-wide --edit: open ~/.onix (or specific files within).
		return (&EditCmd{Files: rest}).Run(ctx, e)
	case "show":
		return (&ShowCmd{Args: rest}).Run(ctx, e)
	case "remove":
		files, force, recursive, err := parseRemoveArgs(rest)
		if err != nil {
			return err
		}
		return (&RemoveCmd{Files: files, Force: force, Recursive: recursive}).Run(ctx, e)
	case "contexts":
		return (&ContextListCmd{}).Run(ctx, e)
	case "init":
		cmd := &InitCmd{}
		for _, a := range rest {
			switch a {
			case "--skip-profile":
				cmd.SkipProfile = true
			default:
				return fmt.Errorf("unknown flag for --init: %q", a)
			}
		}
		return cmd.Run(ctx, e)
	case "sync":
		return (&SyncCmd{}).Run(ctx, e)
	case "doctor":
		return (&DoctorCmd{}).Run(ctx, e)
	case "stats":
		return runStatsFromArgs(ctx, e, rest)
	case "version":
		return (&VersionCmd{}).Run(ctx, e)
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
		return fastResolve(e.Home, alias, false, e.Stdout, e.Stderr, e.Stdin)
	case "remove":
		files, force, recursive, err := parseRemoveArgs(actionArgs)
		if err != nil {
			return err
		}
		return (&RemoveCmd{Alias: alias, Files: files, Force: force, Recursive: recursive}).Run(ctx, e)
	case "edit":
		return (&EditCmd{Alias: alias, Files: actionArgs}).Run(ctx, e)
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
		// We accept "<plugin>:<entry>" so generated wrappers can select an
		// entry without a separate flag. Plain "<plugin>" means no entry.
		spec := actionArgs[0]
		name, entry := spec, ""
		if i := strings.Index(spec, ":"); i >= 0 {
			name = spec[:i]
			entry = spec[i+1:]
		}
		return (&PluginExecCmd{Args: append([]string{name, entry, alias}, actionArgs[1:]...)}).Run(ctx, e)
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
		return fastResolve(e.Home, alias, noPrompt, e.Stdout, e.Stderr, e.Stdin)
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
		return fastResolve(e.Home, alias, noPrompt, e.Stdout, e.Stderr, e.Stdin)
	}
	return add.Run(ctx, e)
}

// parseRemoveArgs splits the argv after --remove/-rm into file positionals
// and the --force/--recursive flags. Long and short forms are both
// accepted. Unknown flags are returned as an error so a typo like
// `--Force` doesn't silently fall through into the files slice and become
// a file to delete.
func parseRemoveArgs(args []string) (files []string, force, recursive bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--force", "-F":
			force = true
		case "--recursive", "-R":
			recursive = true
		default:
			if strings.HasPrefix(a, "-") {
				return nil, false, false, fmt.Errorf("unknown flag for --remove: %q", a)
			}
			files = append(files, a)
		}
	}
	return files, force, recursive, nil
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
