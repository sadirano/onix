//go:build !windows

package main

import "fmt"

// openInExplorer opens the target path in the system file manager.
// On Linux we use xdg-open. macOS is not supported.
//
// Goes through execCommand (rather than exec.Command directly) so tests
// can replace it with a fake — otherwise this branch is unreachable on
// CI runners that don't have xdg-open installed.
func openInExplorer(target string) error {
	bin := "xdg-open"

	// We use Start() rather than Run() so onix doesn't block while the
	// explorer window is open.
	if err := execCommand(bin, target).Start(); err != nil {
		return fmt.Errorf("%s %s: %w", bin, target, err)
	}
	return nil
}

// runCommandOutside starts the command in the background on non-Windows platforms.
func runCommandOutside(dir string, exe string, args []string) error {
	cmd := execCommand(exe, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	return nil
}

