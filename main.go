// Command onix is a fast directory alias resolver.
//
// The hot path is `onix <alias>`: it reads ~/.onix/aliases.toml, looks up
// the alias, and prints the resolved absolute path.
//
// onix ships as a multi-call binary: it is installed into ~/.onix/bin under
// each command name (o, e, r, ...), hardlinked to the same executable, and
// recovers the intended action from argv[0] (see multicall.go). Navigation
// (`o`) opens a fresh shell rooted at the resolved directory — a child process
// can't relocate its parent shell, so it stacks a subshell instead. On
// POSIX, `o` stays a shell function that cd's the calling shell in place.
//
// Other built-in actions (--edit, --explore, --yank, --run, ...) act on the
// resolved directory directly because they don't need to change the calling
// shell's directory.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"

	"github.com/sadirano/onix/internal/resolver"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int) {
	t := newTimer()
	defer t.report()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "\n[onix] CRASH: encountered an unexpected error\n")
			fmt.Fprintf(stderr, "Please report this issue at https://github.com/sadirano/onix/issues/new\n\n")

			stack := make([]byte, 1024)
			for {
				n := runtime.Stack(stack, false)
				if n < len(stack) {
					stack = stack[:n]
					break
				}
				stack = make([]byte, len(stack)*2)
			}

			fmt.Fprintf(stderr, "```markdown\n")
			fmt.Fprintf(stderr, "### Environment\n")
			fmt.Fprintf(stderr, "- Version: %s\n", resolveBuildVersion())
			if commit := resolveBuildCommit(); commit != "" {
				fmt.Fprintf(stderr, "- Commit:  %s\n", commit)
			}
			fmt.Fprintf(stderr, "- GOOS:    %s\n", runtime.GOOS)
			fmt.Fprintf(stderr, "- GOARCH:  %s\n", runtime.GOARCH)
			fmt.Fprintf(stderr, "- Runtime: %s\n\n", runtime.Version())
			fmt.Fprintf(stderr, "### Panic\n%v\n\n", r)
			fmt.Fprintf(stderr, "### Stack Trace\n%s\n", stack)
			fmt.Fprintf(stderr, "```\n")

			exitCode = 1
		}
	}()

	// Argv preprocessing: rewrite multi-char short flags (-ls -> --list,
	// -rm -> --remove) so the dispatcher only has to deal with canonical
	// long forms. Single-rune shorts pass through.
	processedArgs := append([]string{args[0]}, preprocessArgs(args[1:])...)
	t.mark("preprocess")

	// Resolve onix home up front. Every code path needs it.
	home, err := resolveHome(os.Getenv("ONIX_HOME"))
	if err != nil {
		fmt.Fprintf(stderr, "onix: %v\n", err)
		return 1
	}
	t.mark("resolve-home")
	e := &env{
		Home:     home,
		JSON:     hasFlag(processedArgs[1:], "--json", "-j"),
		NoPrompt: hasFlag(processedArgs[1:], "--no-prompt", "-q"),
		Stdout:   stdout,
		Stderr:   stderr,
		Stdin:    stdin,
		Timer:    t,
	}
	if e.NoPrompt {
		os.Setenv("ONIX_NO_PROMPT", "1")
	}

	t.mark("pre-dispatch")

	// Multi-call dispatch: when invoked under a wrapper name (o, e, r, ...),
	// recover the action from argv[0] and either spawn a navigation subshell
	// or desugar into the canonical grammar.
	dispatchArgs := processedArgs[1:]
	if action, ok := invokedAction(home, args[0]); ok {
		rewritten, navAlias, isNav := desugarMultiCall(action, dispatchArgs)
		if isNav {
			return dispatchResult(navigateAndSubshell(sigCtx, e, navAlias, dispatchArgs), stderr)
		}
		dispatchArgs = rewritten
	}

	if err := dispatchNewGrammar(sigCtx, e, dispatchArgs, stdout, stderr); err != nil {
		return dispatchResult(err, stderr)
	}
	t.mark("post-dispatch")

	return 0
}

// dispatchResult maps a handler error to a process exit code, mirroring the
// inline handling the grammar dispatch has always used: child exit codes pass
// through verbatim, a cancelled prompt is silent, anything else prints.
func dispatchResult(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var cee *childExitError
	if errors.As(err, &cee) {
		return cee.Code
	}
	if !errors.Is(err, resolver.ErrCancelled) {
		fmt.Fprintf(stderr, "onix: %v\n", err)
	}
	return 1
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
	Home     string    // absolute path to the onix config directory (~/.onix by default)
	JSON     bool      // whether to output JSON
	NoPrompt bool      // suppress interactive prompts
	Stdout   io.Writer // captured for testing
	Stderr   io.Writer // captured for testing
	Stdin    io.Reader // captured for testing
	Timer    *timer    // checkpoint timer
}
