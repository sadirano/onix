package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/errs"
	"github.com/sadirano/onix/internal/opener"
)

// executeAction carries out the resolved action against target.
// Returns *errs.ExitError when a child process exits with a non-zero code so
// that main can forward the exit code without calling os.Exit from a library.
func executeAction(action, target string, extras []string, cfg *config.Config, t *timer) error {
	switch action {
	case "explorer":
		return openExplorer(target)

	case "editor":
		return opener.RunEditorCommand(cfg.ResolveEditor(), target, ".")

	case "print":
		fmt.Println(target)
		// clip.exe is built-in on Windows.
		c := exec.Command("clip")
		c.Stdin = strings.NewReader(target)
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not copy to clipboard: %v\n", err)
		}
		// setx sets a user-level environment variable.
		if err := exec.Command("setx", "ONIX_LAST", target).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not set ONIX_LAST: %v\n", err)
		}
		return nil

	case "files":
		if len(extras) == 0 || (len(extras) == 1 && extras[0] == ".") {
			return opener.RunEditorCommand(cfg.ResolveEditor(), target, ".")
		}
		resolved := make([]string, len(extras))
		for i, arg := range extras {
			if filepath.IsAbs(arg) {
				resolved[i] = arg
			} else {
				resolved[i] = filepath.Join(target, arg)
			}
		}
		return opener.OpenMixedFiles(cfg.ResolveEditor(), resolved)

	case "run":
		if cfg.Settings.DisableRun {
			return fmt.Errorf("run builtin is disabled (disable_run = true in config)")
		}
		if len(extras) == 0 {
			return fmt.Errorf("usage: run <alias> \"<command>\"")
		}
		var rcmd *exec.Cmd
		if isUNCPath(target) {
			wrapped := fmt.Sprintf(`pushd "%s" && %s`, target, strings.Join(extras, " "))
			rcmd = exec.Command("cmd.exe", "/C", wrapped)
		} else {
			rcmd = exec.Command("cmd.exe", "/C", strings.Join(extras, " "))
			rcmd.Dir = target
		}
		rcmd.Stdout = os.Stdout
		rcmd.Stderr = os.Stderr
		rcmd.Stdin = os.Stdin
		if err := rcmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ProcessState != nil {
				return &errs.ExitError{Code: exitErr.ProcessState.ExitCode()}
			}
			return fmt.Errorf("run command: %w", err)
		}
		return nil

	default: // "shell" and anything unrecognised
		t.mark("shell spawned")
		return openShellAt(target)
	}
}

