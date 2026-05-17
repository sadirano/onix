package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ShowCmd displays file contents (or directory listings) without launching
// an editor. It's the read-only counterpart to --edit.
//
// Args are passed through to the platform-native viewer so users can use
// flags they already know:
//   - Windows:   Get-Content / Get-ChildItem (PowerShell)
//   - Unix/mac:  cat / ls
//
// If Args contains no positional (filename) the command treats it as a
// directory listing. Otherwise it shows the named files.
type ShowCmd struct {
	// Alias is empty for the system-wide form (operates on ~/.onix).
	Alias string

	// Args is the raw argv slice after the --show flag. The dispatcher
	// passes these through verbatim so flags like -Head 20 / -Tail 50 /
	// -A reach the underlying tool unmolested.
	Args []string
}

// Run executes the show command.
func (c *ShowCmd) Run(ctx context.Context, e *env) error {
	dir, err := c.targetDir(e)
	if err != nil {
		return err
	}

	if !hasPositional(c.Args) {
		return runShowList(ctx, dir, c.Args)
	}
	return runShowFiles(ctx, dir, c.Args)
}

func (c *ShowCmd) targetDir(e *env) (string, error) {
	if c.Alias == "" {
		return e.Home, nil
	}
	target, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return "", err
	}
	return target, nil
}

// hasPositional reports whether args contains at least one non-flag token.
// A "flag" is anything starting with '-'. Values that follow flags are
// indistinguishable from filenames here, but the underlying tool sorts that
// out — we just need to know if the user gave us anything to display.
func hasPositional(args []string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return true
		}
	}
	return false
}

// runShowList lists directory contents. On Windows this shells out to
// PowerShell's Get-ChildItem; on Unix it calls ls -la. Extra args are
// forwarded so users can pass -Filter / -Recurse / -la / etc.
func runShowList(ctx context.Context, dir string, args []string) error {
	cmd, err := buildShowCommand(ctx, "list", args)
	if err != nil {
		return err
	}
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return passthroughExit(cmd.Run())
}

// runShowFiles displays file contents using Get-Content (Windows) or cat
// (Unix). Args (filenames + tool flags) are forwarded to the child.
func runShowFiles(ctx context.Context, dir string, args []string) error {
	cmd, err := buildShowCommand(ctx, "files", args)
	if err != nil {
		return err
	}
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return passthroughExit(cmd.Run())
}

// buildShowCommand picks the right native viewer for the platform and
// returns an exec.Cmd that hasn't been started yet (so the caller can
// still set Dir / Stdio).
//
// mode is "list" (directory) or "files" (cat). args are forwarded
// verbatim — filenames with spaces or special chars must be single-quoted
// for PowerShell. We auto-quote any positional containing whitespace so
// the common case (one filename) just works.
func buildShowCommand(ctx context.Context, mode string, args []string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		verb := "Get-Content"
		if mode == "list" {
			verb = "Get-ChildItem"
		}
		script := verb
		if len(args) > 0 {
			script += " " + strings.Join(psQuoteArgs(args), " ")
		}
		return execCommandContext(
			ctx,
			"powershell",
			"-NoProfile", "-NonInteractive", "-Command", script,
		), nil
	}

	bin := "cat"
	if mode == "list" {
		bin = "ls"
		// Reasonable default — long listing matches what most users want
		// when they just type `onix --show`. Extra user args override or
		// supplement these.
		if !hasShortFlag(args, "l") {
			args = append([]string{"-la"}, args...)
		}
	}
	return execCommandContext(ctx, bin, args...), nil
}

// psQuoteArgs single-quotes any arg containing whitespace or PowerShell
// metachars. Flags (anything starting with '-') and bare identifiers are
// left untouched so `-Head 5` reaches Get-Content as two tokens.
func psQuoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.HasPrefix(a, "-") || !strings.ContainsAny(a, " \t\"'`$;,") {
			out[i] = a
			continue
		}
		out[i] = "'" + strings.ReplaceAll(a, "'", "''") + "'"
	}
	return out
}

// hasShortFlag reports whether args contains a single-char flag like -l or
// -la (matches anywhere in clustered shorts). Used to skip the default
// `ls -la` when the user supplied their own listing flags.
func hasShortFlag(args []string, ch string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") {
			continue
		}
		if strings.Contains(a[1:], ch) {
			return true
		}
	}
	return false
}

var exit = os.Exit

// passthroughExit propagates child exit codes verbatim so failures (e.g.
// missing file) surface with the same status the user would see from
// running Get-Content / cat directly.
func passthroughExit(err error) error {
	if err == nil {
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		exit(ee.ExitCode())
	}
	return fmt.Errorf("show: %w", err)
}
