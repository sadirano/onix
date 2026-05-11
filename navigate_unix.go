//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func launchedFromRunner() bool { return false }

func openShellAt(dir, shell string) error {
	if shell == "" {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
	}
	cmd := exec.Command(shell)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
