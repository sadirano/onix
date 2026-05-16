// Command onix is a fast directory alias resolver.
//
// The hot path is `onix resolve <alias>`: it reads ~/.onix/aliases.toml,
// looks up the alias, and prints the resolved absolute path. Shell integration
// (a PowerShell function named `o`) wraps that output in a Set-Location call so
// `o acme` changes the current shell's directory with no new process spawn.
//
// Other built-in actions (edit/explore/yank/run) are invoked directly on the
// onix binary because they don't need to change the calling shell's directory.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"github.com/alecthomas/kong"
	"github.com/sadirano/onix/internal/resolver"
)

// CLI is the top-level kong grammar. Each field is a subcommand whose tag
// describes its behaviour in --help. Keep this struct small and delegate
// every Run() to a helper in its own file so main.go stays readable.
type CLI struct {
	// Hot path: print resolved path to stdout. Shell functions read this.
	Resolve ResolveCmd `cmd:"" help:"Print the resolved absolute path of an alias." examples:"onix resolve web"`

	// Alias management.
	Add     AddCmd     `cmd:"" help:"Register or update an alias." examples:"onix add web C:/projects/web"`
	Remove  RemoveCmd  `cmd:"" aliases:"rm" help:"Remove an alias." examples:"onix rm web"`
	List    ListCmd    `cmd:"" aliases:"ls" help:"List all aliases." examples:"onix ls"`
	Aliases AliasesCmd `cmd:"" help:"Open the aliases.toml file in your editor." examples:"onix aliases"`

	// Actions that operate on the resolved directory.
	Edit    EditCmd    `cmd:"" help:"Open the alias directory in your editor." examples:"onix edit web"`
	Grep    GrepCmd    `cmd:"" help:"Search file contents in an alias directory using ripgrep and fzf." examples:"onix grep web \"some query\""`
	Find    FindCmd    `cmd:"" help:"Find files in an alias directory using Everything/fd and fzf." examples:"onix find web \"some file\""`
	Explore ExploreCmd `cmd:"" help:"Open the alias directory in the OS file manager." examples:"onix explore web"`
	Yank    YankCmd    `cmd:"" help:"Print the alias path and copy it to the clipboard." examples:"onix yank web"`
	Run     RunCmd     `cmd:"" passthrough:"" help:"Run a command in the alias directory." examples:"onix run web \"npm start\""`

	// Custom action dispatch (declared in ~/.onix/config.toml).
	Exec ExecCmd `cmd:"" passthrough:"" help:"Run a configured action against an alias." examples:"onix exec test web"`

	// External plugin management + runtime dispatch.
	Plugin     PluginCmd     `cmd:"" help:"Install and manage external plugins."`
	PluginExec PluginExecCmd `cmd:"" name:"plugin-exec" passthrough:"" help:"Internal: run a plugin against an alias (called by generated shell wrappers)."`

	Context ContextCmd `cmd:"" help:"Manage segment contexts."`

	// Installation and diagnostics.
	Init           InitCmd           `cmd:"" help:"Create ~/.onix and install shell integration." examples:"onix init"`
	Sync           SyncCmd           `cmd:"" help:"Regenerate shell snippets and Windows wrappers." examples:"onix sync"`
	ListNames      ListNamesCmd      `cmd:"" name:"list-names" help:"Print alias names (used by tab-completion)."`
	Doctor         DoctorCmd         `cmd:"" help:"Check installation health." examples:"onix doctor"`
	Stats          StatsCmd          `cmd:"" help:"Report local navigation patterns from your usage log." examples:"onix stats\nonix stats --full\nonix stats --cold --since 30d"`
	Version        VersionCmd        `cmd:"" help:"Print the onix version." examples:"onix version"`

	// Global flags.
	ConfigDir string `name:"config-dir" env:"ONIX_HOME" help:"Override ~/.onix location."`
	JSON      bool   `name:"json" help:"Output in JSON format."`
}

func main() {
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n[onix] CRASH: encountered an unexpected error\n")
			fmt.Fprintf(os.Stderr, "Please report this issue at https://github.com/sadirano/onix/issues/new\n\n")

			stack := make([]byte, 1024)
			for {
				n := runtime.Stack(stack, false)
				if n < len(stack) {
					stack = stack[:n]
					break
				}
				stack = make([]byte, len(stack)*2)
			}

			fmt.Fprintf(os.Stderr, "```markdown\n")
			fmt.Fprintf(os.Stderr, "### Environment\n")
			fmt.Fprintf(os.Stderr, "- Version: %s\n", resolveBuildVersion())
			if commit := resolveBuildCommit(); commit != "" {
				fmt.Fprintf(os.Stderr, "- Commit:  %s\n", commit)
			}
			fmt.Fprintf(os.Stderr, "- GOOS:    %s\n", runtime.GOOS)
			fmt.Fprintf(os.Stderr, "- GOARCH:  %s\n", runtime.GOARCH)
			fmt.Fprintf(os.Stderr, "- Runtime: %s\n\n", runtime.Version())
			fmt.Fprintf(os.Stderr, "### Panic\n%v\n\n", r)
			fmt.Fprintf(os.Stderr, "### Stack Trace\n%s\n", stack)
			fmt.Fprintf(os.Stderr, "```\n")

			os.Exit(1)
		}
	}()

	// Argv preprocessing: rewrite multi-char short flags (-ls -> --list,
	// -rm -> --remove) before any further parsing. Kong only handles
	// single-rune shorts natively, so we normalise here.
	os.Args = append([]string{os.Args[0]}, preprocessArgs(os.Args[1:])...)

	// Hot-path shortcuts. Two commands run on every keystroke:
	//   - `onix resolve <name>` — fires when the user presses Enter on `o`
	//   - `onix list-names`     — fires on every Tab press during completion
	// Both bypass kong's reflection-based grammar setup (~2–3ms) so the
	// only overhead is process spawn + file read + scan.
	if len(os.Args) >= 3 && os.Args[1] == "resolve" {
		if name, noPrompt, ok := parseFastResolveArgs(os.Args[2:]); ok {
			home, err := resolveHome(os.Getenv("ONIX_HOME"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "onix: %v\n", err)
				os.Exit(1)
			}
			if err := fastResolve(home, name, noPrompt); err != nil {
				if !noPrompt && !errors.Is(err, resolver.ErrCancelled) {
					fmt.Fprintf(os.Stderr, "onix: %v\n", err)
				}
				os.Exit(1)
			}
			return
		}
	}
	if len(os.Args) == 2 && os.Args[1] == "list-names" {
		home, err := resolveHome(os.Getenv("ONIX_HOME"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "onix: %v\n", err)
			os.Exit(1)
		}
		if err := fastListNames(home); err != nil {
			if !errors.Is(err, resolver.ErrCancelled) {
				fmt.Fprintf(os.Stderr, "onix: %v\n", err)
			}
			os.Exit(1)
		}
		return
	}

	// New alias-flag grammar dispatcher. Handles:
	//   onix <alias> [<path>] [--description X] [--owner X] [--tags X...]
	//   onix <alias> --<action> [args...]
	//   onix --<system-action> [args...]
	// Falls through to kong (handled=false) when the user typed an old
	// subcommand form (e.g. `onix resolve foo`, `onix plugin add ...`), so
	// installed shell snippets keep working until they re-sync.
	{
		home, herr := resolveHome(os.Getenv("ONIX_HOME"))
		if herr == nil {
			// Build a minimal env for the new dispatcher. JSON is
			// detected on a best-effort basis; the legacy kong path
			// handles `--json` for unknown shapes.
			e := &env{Home: home, JSON: hasFlag(os.Args[1:], "--json", "-j")}
			handled, derr := tryDispatchNewGrammar(sigCtx, e, os.Args[1:])
			if handled {
				if derr != nil {
					if !errors.Is(derr, resolver.ErrCancelled) {
						fmt.Fprintf(os.Stderr, "onix: %v\n", derr)
					}
					os.Exit(1)
				}
				return
			}
		}
	}

	var cli CLI
	ctx := kong.Parse(
		&cli,
		kong.Name("onix"),
		kong.Description("Fast directory alias resolver."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)

	// Resolve the onix home directory once and stash it on the context so
	// every subcommand can read it without redoing the env lookup.
	home, err := resolveHome(cli.ConfigDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onix: %v\n", err)
		os.Exit(1)
	}
	ctx.Bind(&env{
		Home: home,
		JSON: cli.JSON,
	})
	ctx.BindTo(sigCtx, (*context.Context)(nil))

	if err := ctx.Run(); err != nil {
		// Subcommands return errors instead of calling os.Exit so kong can
		// print them consistently and tests can assert on them.
		if !errors.Is(err, resolver.ErrCancelled) {
			fmt.Fprintf(os.Stderr, "onix: %v\n", err)
		}
		os.Exit(1)
	}
}

// startsWithDash is a cheap helper to spot flags so the hot path stays safe.
// If anyone ever passes `onix resolve -h` we want kong's help to fire, not
// the fast path silently treating `-h` as an alias name.
func startsWithDash(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// hasFlag reports whether any of names appears as a literal token in args.
// Used to detect --json on a best-effort basis before the full dispatcher
// has parsed argv. It does not understand `--json=value` (--json is a bool).
func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

// parseFastResolveArgs picks the alias name and --no-prompt flag out of the
// args for `onix resolve ...`. Returns ok=false for anything else (other
// flags, --help, two positionals, etc.) so the slow kong path handles it.
func parseFastResolveArgs(args []string) (name string, noPrompt bool, ok bool) {
	for _, a := range args {
		if a == "--no-prompt" {
			noPrompt = true
			continue
		}
		if startsWithDash(a) || name != "" {
			return "", false, false
		}
		name = a
	}
	return name, noPrompt, name != ""
}

// env carries process-wide settings into every subcommand via kong.Bind.
// Keep it small — anything that's really per-subcommand belongs on that
// subcommand's struct as a flag.
type env struct {
	Home string // absolute path to the onix config directory (~/.onix by default)
	JSON bool   // whether to output JSON
}
