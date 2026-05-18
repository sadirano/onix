package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/opener"
)

// executeAction carries out the resolved action against target.
func executeAction(action, target string, extras []string, cfg *config.Config, t *timer) {
	switch action {
	case "e":
		if err := opener.OpenInExplorer(target); err != nil {
			fatal("open explorer: %v", err)
		}

	case "n":
		if err := opener.RunEditorCommand(cfg.ResolveEditor(), target, "."); err != nil {
			fatal("%v", err)
		}

	case "y":
		fmt.Println(target)

	case "f":
		if len(extras) == 0 || (len(extras) == 1 && extras[0] == ".") {
			if err := opener.RunEditorCommand(cfg.ResolveEditor(), target, "."); err != nil {
				fatal("%v", err)
			}
		} else {
			resolved := make([]string, len(extras))
			for i, arg := range extras {
				if filepath.IsAbs(arg) {
					resolved[i] = arg
				} else {
					resolved[i] = filepath.Join(target, arg)
				}
			}
			if err := opener.OpenMixedFiles(cfg.ResolveEditor(), resolved); err != nil {
				fatal("%v", err)
			}
		}

	case "r":
		if len(extras) == 0 {
			fatal("usage: onix <alias> -r \"<command>\"")
		}
		var rcmd *exec.Cmd
		if isUNCPath(target) {
			escapedTarget := strings.ReplaceAll(target, `"`, `\"`)
			wrapped := fmt.Sprintf(`pushd "%s" && %s`, escapedTarget, strings.Join(extras, " "))
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
				os.Exit(exitErr.ProcessState.ExitCode())
			}
			fatal("run command: %v", err)
		}

	default:
		t.mark("shell spawned")
		if err := openShellAt(target); err != nil {
			fatal("open shell: %v", err)
		}
	}
}

