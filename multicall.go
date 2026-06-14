package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// onix ships as a multi-call binary: the same executable is hardlinked into
// ~/.onix/bin under each command name (o, e, r, ...). When invoked under one
// of those names we read argv[0] to recover which action the user meant,
// rather than requiring an explicit --flag.

// slotAction maps a builtin shortcut slot to the action its wrapper performs.
// The slot names match config.BuiltinDefaults keys; the values are the
// canonical action names used throughout dispatch.
var slotAction = map[string]string{
	"o":  "navigate",
	"e":  "edit",
	"s":  "explore",
	"y":  "yank",
	"p":  "paste",
	"r":  "run",
	"sg": "grep",
	"ff": "find",
}

// actionFlag maps a non-navigate action to the alias-flag it desugars to, so
// multi-call invocations reuse the exact dispatchNewGrammar action paths
// instead of duplicating their argument handling.
var actionFlag = map[string]string{
	"edit":    "--edit",
	"explore": "--explore",
	"yank":    "--yank",
	"paste":   "--paste",
	"run":     "--run",
	"grep":    "--grep",
	"find":    "--find",
}

// invokedAction inspects argv0's basename to decide whether onix was launched
// under one of its command wrapper names. It returns the action and true when
// so; ("", false) means a plain `onix` invocation that should go through the
// normal grammar. A user remap (config.Shortcuts maps slot -> custom name) is
// resolved by reverse lookup; the config read only happens for names that
// aren't a builtin slot already, so default installs stay allocation-cheap on
// the hot path.
func invokedAction(home, argv0 string) (string, bool) {
	name := wrapperName(argv0)
	if name == "" || name == "onix" {
		return "", false
	}
	slot := name
	if !config.IsBuiltinName(slot) {
		if cfg, err := config.LoadConfig(home); err == nil {
			for builtin, custom := range cfg.Shortcuts {
				if strings.EqualFold(custom, name) {
					slot = builtin
					break
				}
			}
		}
	}
	action, ok := slotAction[slot]
	return action, ok
}

// wrapperName normalises argv0 to a lowercase basename with any .exe suffix
// stripped, so "C:\...\O.EXE" and "/usr/local/bin/o" both yield "o".
func wrapperName(argv0 string) string {
	base := strings.ToLower(filepath.Base(argv0))
	return strings.TrimSuffix(base, ".exe")
}

// desugarMultiCall turns a wrapper invocation into the canonical grammar argv.
// It returns (rewritten args, navigateAlias, isNavigate). For navigate with a
// real alias, isNavigate is true and the caller spawns a subshell; every other
// case yields args ready for dispatchNewGrammar.
//
// Convention: no args opens the config editor; a leading dash is passed
// straight through as a system verb (-v, --help, ...).
func desugarMultiCall(action string, args []string) (rewritten []string, navAlias string, isNav bool) {
	if len(args) == 0 {
		if action == "navigate" {
			// Bare `o` opens the onix config editor.
			return []string{"--edit"}, "", false
		}
		// Bare `e`/`r`/... has no alias to act on; let the grammar print usage.
		return nil, "", false
	}
	if startsWithDash(args[0]) {
		// System verb / global flag passthrough (`o -v`, `o --help`).
		return args, "", false
	}
	alias := args[0]
	rest := args[1:]
	if action == "navigate" {
		return nil, alias, true
	}
	out := make([]string, 0, len(rest)+2)
	out = append(out, alias, actionFlag[action])
	out = append(out, rest...)
	return out, "", false
}

// navigateAndSubshell resolves the alias and opens a fresh interactive shell
// rooted in the target directory. A child process can't relocate its parent
// shell, so onix-as-a-real-exe navigates by stacking a subshell instead; the
// user returns to the original shell by exiting it.
//
// An unknown plain alias falls back to the directory picker inside
// resolveAliasPath (shared with every other action), so navigation registers
// on the fly.
func navigateAndSubshell(ctx context.Context, e *env, alias string, rest []string) error {
	// rest may carry global flags (-q/-j) already parsed into env; navigation
	// itself takes no positional args beyond the alias, so they're ignored.
	target, err := resolveAliasPath(e, alias)
	if err != nil {
		return err
	}
	return spawnSubshell(ctx, e, target)
}

// spawnSubshell launches an interactive shell with its working directory set
// to dir and the parent console inherited. Exit codes are propagated via
// childExitError so main's deferred recovery still runs before os.Exit.
func spawnSubshell(ctx context.Context, e *env, dir string) error {
	shell, args := interactiveShell()
	cmd := execCommandContext(ctx, shell, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = e.Stdout
	cmd.Stderr = e.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return &childExitError{Code: ee.ExitCode()}
		}
		return fmt.Errorf("open subshell %q: %w", shell, err)
	}
	return nil
}

// interactiveShell picks the shell to spawn for navigation. ONIX_SHELL wins so
// users can pin one explicitly; otherwise we fall back to the platform's
// conventional interactive shell ($COMSPEC on Windows, $SHELL on POSIX).
func interactiveShell() (string, []string) {
	if s := strings.TrimSpace(os.Getenv("ONIX_SHELL")); s != "" {
		return s, nil
	}
	if runtime.GOOS == "windows" {
		if c := strings.TrimSpace(os.Getenv("COMSPEC")); c != "" {
			return c, nil
		}
		return "cmd.exe", nil
	}
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return s, nil
	}
	return "/bin/sh", nil
}
