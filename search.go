package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// -----------------------------------------------------------------------------
// grep — search file contents using ripgrep and fzf.
// -----------------------------------------------------------------------------

type GrepCmd struct {
	Args []string `arg:"" name:"args" help:"<alias> [query] [extras...]"`
}

func (c *GrepCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 1 {
		return fmt.Errorf("usage: onix grep <alias> [query] [extras...]")
	}
	alias := c.Args[0]
	query := ""
	if len(c.Args) > 1 {
		query = c.Args[1]
	}
	extras := c.Args[2:]

	target, err := resolveAliasPath(e, alias)
	if err != nil {
		return err
	}

	if _, err := lookPath("rg"); err != nil {
		return fmt.Errorf("ripgrep ('rg') not found on PATH")
	}
	if _, err := lookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found on PATH")
	}

	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}

	// rg output is file:line:text — fzf splits on `:`, so {1}=file and
	// {2}=line in the preview command. No trailing "." so rg prints
	// "src/foo.go:" rather than "./src/foo.go:". rgCmd.Stdin is set to
	// the parent's tty below so rg falls back to "search cwd" instead
	// of reading patterns from a nil stdin.
	rgArgs := []string{"--smart-case", "--color=always", "--line-number", "--no-heading"}
	for _, spec := range cfg.Grep.RgColorsOrDefault() {
		rgArgs = append(rgArgs, "--colors", spec)
	}
	rgArgs = append(rgArgs, extras...)
	if query != "" {
		rgArgs = append(rgArgs, query)
	}

	fzfArgs := []string{"--ansi", "--multi"}
	if strings.TrimSpace(cfg.Grep.FzfColors) != "" {
		fzfArgs = append(fzfArgs, "--color", cfg.Grep.FzfColors)
	}
	fzfArgs = append(
		fzfArgs,
		"--delimiter", ":",
		"--preview", cfg.Grep.PreviewCommandOrDefault(),
		"--preview-window", cfg.Grep.PreviewWindowOrDefault(),
	)

	rgCmd := execCommandContext(ctx, "rg", rgArgs...)
	rgCmd.Dir = target
	rgCmd.Stdin = os.Stdin
	rgOut, err := rgCmd.StdoutPipe()
	if err != nil {
		return err
	}

	fzfCmd := execCommandContext(ctx, "fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = rgOut
	fzfCmd.Stderr = os.Stderr // fzf UI uses stderr when stdout is captured
	applyDefaultFzfTheme(fzfCmd)

	if err := rgCmd.Start(); err != nil {
		return fmt.Errorf("start rg: %w", err)
	}

	selected, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil // User cancelled fzf
		}
		// fzf returns exit code 1 if nothing selected, but we should distinguish between error and no selection.
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(selected)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil
	}

	return openSelectionsInEditor(ctx, target, lines, true)
}

// -----------------------------------------------------------------------------
// find — find files using Everything/fd and fzf.
// -----------------------------------------------------------------------------

type FindCmd struct {
	Args []string `arg:"" name:"args" help:"<alias> [query] [extras...]"`
}

func (c *FindCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 1 {
		return fmt.Errorf("usage: onix find <alias> [query] [extras...]")
	}
	alias := c.Args[0]
	query := ""
	if len(c.Args) > 1 {
		query = c.Args[1]
	}
	extras := c.Args[2:]

	target, err := resolveAliasPath(e, alias)
	if err != nil {
		return err
	}

	if _, err := lookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found on PATH")
	}

	var findCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		if _, err := lookPath("es"); err == nil {
			esArgs := []string{"--path", "./"}
			if query != "" {
				esArgs = append(esArgs, query)
			}
			esArgs = append(esArgs, extras...)
			findCmd = execCommandContext(ctx, "es", esArgs...)
			findCmd.Dir = target
		}
	}

	if findCmd == nil {
		if _, err := lookPath("fd"); err == nil {
			fdArgs := []string{"--type", "f", "--color", "always"}
			fdArgs = append(fdArgs, extras...)
			if query != "" {
				fdArgs = append(fdArgs, query)
			}
			findCmd = execCommandContext(ctx, "fd", fdArgs...)
			findCmd.Dir = target
		} else {
			// Fallback to find
			findArgs := []string{".", "-type", "f"}
			if query != "" {
				findArgs = append(findArgs, "-name", "*"+query+"*")
			}
			findArgs = append(findArgs, extras...)
			findCmd = execCommandContext(ctx, "find", findArgs...)
			findCmd.Dir = target
		}
	}

	findOut, err := findCmd.StdoutPipe()
	if err != nil {
		return err
	}

	fzfArgs := []string{
		"--ansi",
		"--multi",
		"--preview", findPreviewCommand(),
	}
	fzfCmd := execCommandContext(ctx, "fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = findOut
	fzfCmd.Stderr = os.Stderr
	applyDefaultFzfTheme(fzfCmd)

	if err := findCmd.Start(); err != nil {
		return fmt.Errorf("start finder: %w", err)
	}

	selected, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil
		}
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(selected)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil
	}

	return openSelectionsInEditor(ctx, target, lines, false)
}

// findPreviewCommand returns the fzf --preview command for `ff`, which
// must handle both files (bat) and directories (listing). es returns
// directories alongside files; bat errors on a directory path, so a
// flat `bat {}` breaks the preview pane the moment the user lands on a
// folder. We branch in the shell that fzf invokes — that's cmd.exe on
// Windows and /bin/sh elsewhere.
func findPreviewCommand() string {
	if runtime.GOOS == "windows" {
		// cmd.exe trick: `<path>\.` only resolves when <path> is a
		// directory, so `if exist "{}\."` is the portable batch test
		// for "is this a folder". /b keeps `dir` to one filename per
		// line so the preview pane isn't overwhelmed.
		return `if exist "{}\." (dir /b "{}") else (bat --style=numbers --color=always "{}")`
	}
	// POSIX sh: -d tests for a directory.
	return `if [ -d "{}" ]; then ls -la "{}"; else bat --style=numbers --color=always "{}"; fi`
}

// fzfTokyoNightTheme is the default --color set we hand fzf via
// FZF_DEFAULT_OPTS when the user hasn't set one. Whitespace (including
// newlines) is fine inside FZF_DEFAULT_OPTS — fzf treats it as an
// argument separator.
const fzfTokyoNightTheme = "" +
	"--color=fg:#c0caf5,bg:#1a1b26,hl:#2ac3de,fg+:#c0caf5,bg+:#283457 " +
	"--color=hl+:#2ac3de,info:#7aa2f7,prompt:#2ac3de,pointer:#ff007c " +
	"--color=marker:#ff5da0,spinner:#ff007c,header:#ff9e64,query:#c0caf5 " +
	"--color=border:#27a1b9,separator:#ff9e64,gutter:#283457"

// applyDefaultFzfTheme installs the Tokyo Night palette for fzf via
// FZF_DEFAULT_OPTS, but only if the parent process hasn't already set
// it — a user's own theme always wins.
func applyDefaultFzfTheme(cmd *exec.Cmd) {
	if os.Getenv("FZF_DEFAULT_OPTS") != "" {
		return
	}
	cmd.Env = append(os.Environ(), "FZF_DEFAULT_OPTS="+fzfTokyoNightTheme)
}

func openSelectionsInEditor(ctx context.Context, target string, selections []string, hasLineNumbers bool) error {
	ed := resolveEditor()

	// grep selections are <relative file>:<line>:<text>; find selections
	// are just the file path. On Windows, find can return drive-letter
	// paths ("C:\..."), which themselves contain a colon — so we can't
	// detect the format from the split alone. The caller tells us.
	argv := []string{}
	for _, s := range selections {
		if hasLineNumbers {
			parts := strings.SplitN(s, ":", 3)
			if len(parts) >= 2 {
				argv = append(argv, fmt.Sprintf("+%s", parts[1]), parts[0])
				continue
			}
		}
		argv = append(argv, s)
	}

	if len(argv) == 0 {
		return nil
	}

	cmd := execCommandContext(ctx, ed, argv...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
