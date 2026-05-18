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
	// {2}=line in the preview command. `.` anchors the search at cmd.Dir
	// even when our stdin is nil (otherwise rg would read patterns from
	// stdin).
	rgArgs := []string{cfg.Grep.RipgrepCaseFlag(), "--color=always", "--line-number", "--no-heading"}
	rgArgs = append(rgArgs, extras...)
	if query != "" {
		rgArgs = append(rgArgs, query)
	}
	rgArgs = append(rgArgs, ".")

	fzfArgs := []string{"--ansi", "--multi"}
	if colors := cfg.Grep.FzfColorsOrDefault(); colors != "" {
		fzfArgs = append(fzfArgs, "--color", colors)
	}
	fzfArgs = append(
		fzfArgs,
		"--delimiter", ":",
		"--preview", cfg.Grep.PreviewCommandOrDefault(),
		"--preview-window", cfg.Grep.PreviewWindowOrDefault(),
	)

	rgCmd := execCommandContext(ctx, "rg", rgArgs...)
	rgCmd.Dir = target
	rgOut, err := rgCmd.StdoutPipe()
	if err != nil {
		return err
	}

	fzfCmd := execCommandContext(ctx, "fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = rgOut
	fzfCmd.Stderr = os.Stderr // fzf UI uses stderr when stdout is captured

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

	return openSelectionsInEditor(ctx, target, lines)
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

	previewCmd := "bat --style=numbers --color=always {} 2>/dev/null || cat {}"
	if runtime.GOOS == "windows" {
		previewCmd = "bat --style=numbers --color=always {} 2>$null || type {}"
	}

	fzfArgs := []string{
		"--ansi",
		"--multi",
		"--preview", previewCmd,
	}
	fzfCmd := execCommandContext(ctx, "fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = findOut
	fzfCmd.Stderr = os.Stderr

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

	return openSelectionsInEditor(ctx, target, lines)
}

func openSelectionsInEditor(ctx context.Context, target string, selections []string) error {
	ed := resolveEditor()

	// grep selections are file:line:text; find selections are just file.
	argv := []string{}
	for _, s := range selections {
		parts := strings.Split(s, ":")
		if len(parts) >= 2 {
			argv = append(argv, fmt.Sprintf("+%s", parts[1]), parts[0])
		} else {
			argv = append(argv, s)
		}
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
