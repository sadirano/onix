// Command onix is a fast directory alias resolver.
//
// The hot path is `onix <alias>`: it reads ~/.onix/aliases.toml, looks up
// the alias, and prints the resolved absolute path. Shell integration (a
// PowerShell function named `o`) wraps that output in a Set-Location call
// so `o acme` changes the current shell's directory with no new process
// spawn.
//
// Other built-in actions (--edit, --explore, --yank, --run, ...) are
// invoked directly on the onix binary because they don't need to change
// the calling shell's directory.
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

// pluginCLI is the only kong subtree we keep. Plugin install/update/remove/list
// have their own argv shape (positional repo, --sha, --unpinned, --yes ...)
// that doesn't fit the alias-flag grammar, so they stay as a real subcommand.
// Everything else is parsed by the alias-flag dispatcher in grammar.go.
type pluginCLI struct {
	Plugin PluginCmd `cmd:"" help:"Install and manage external plugins."`

	ConfigDir string `name:"config-dir" env:"ONIX_HOME" help:"Override ~/.onix location."`
	JSON      bool   `name:"json" help:"Output in JSON format."`
}

func main() {
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n[onix] CRASH: encountered an unexpected error\n")
			fmt.Fprintf(os.Stderr, "Please report this issue at https://github.com/sadirano/issues/new\n\n")

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
	// -rm -> --remove) so the dispatcher only has to deal with canonical
	// long forms. Single-rune shorts pass through.
	os.Args = append([]string{os.Args[0]}, preprocessArgs(os.Args[1:])...)

	// Resolve onix home up front. Every code path needs it.
	home, err := resolveHome(os.Getenv("ONIX_HOME"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "onix: %v\n", err)
		os.Exit(1)
	}
	e := &env{
		Home: home,
		JSON: hasFlag(os.Args[1:], "--json", "-j"),
	}

	// Plugin management is the only subcommand-shaped invocation. Route it
	// to kong; everything else flows through the alias-flag dispatcher.
	if len(os.Args) >= 2 && os.Args[1] == "plugin" {
		runPluginKong(sigCtx, e)
		return
	}

	// Dispatch the new alias-flag grammar.
	if err := dispatchNewGrammar(sigCtx, e, os.Args[1:]); err != nil {
		if !errors.Is(err, resolver.ErrCancelled) {
			fmt.Fprintf(os.Stderr, "onix: %v\n", err)
		}
		os.Exit(1)
	}
}

// runPluginKong runs kong against the plugin subtree only. Splitting this
// out keeps main() small and makes it obvious that kong's scope is now
// limited to plugin management.
func runPluginKong(sigCtx context.Context, e *env) {
	var cli pluginCLI
	ctx := kong.Parse(
		&cli,
		kong.Name("onix"),
		kong.Description("Install and manage onix plugins."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	// Allow --config-dir to override the resolved home for this run.
	if cli.ConfigDir != "" {
		home, err := resolveHome(cli.ConfigDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "onix: %v\n", err)
			os.Exit(1)
		}
		e.Home = home
	}
	if cli.JSON {
		e.JSON = true
	}
	ctx.Bind(e)
	ctx.BindTo(sigCtx, (*context.Context)(nil))

	if err := ctx.Run(); err != nil {
		if !errors.Is(err, resolver.ErrCancelled) {
			fmt.Fprintf(os.Stderr, "onix: %v\n", err)
		}
		os.Exit(1)
	}
}

// startsWithDash is the cheap flag-vs-positional check used by the
// dispatcher and several helpers.
func startsWithDash(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// hasFlag reports whether any of names appears as a literal token in args.
// Used to detect --json before the full dispatcher parses argv. It does
// not understand `--json=value` (--json is a bool).
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

// env carries process-wide settings into every command handler.
// Keep it small — anything that's really per-command belongs on that
// command's struct as a flag.
type env struct {
	Home string // absolute path to the onix config directory (~/.onix by default)
	JSON bool   // whether to output JSON
}
