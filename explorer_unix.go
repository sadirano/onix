//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// openInExplorer is a placeholder on non-Windows platforms. M1 is
// Windows-first per the rework scope; Unix support lands in a later
// milestone with xdg-open / open(1). We return a hard error rather than
// silently no-op so anyone running this on Linux knows the action isn't
// wired yet.
func openInExplorer(target string) error {
	// We use Start() rather than Run() so onix doesn't block while the
	// explorer window is open.
	return exec.Command("xdg-open", target).Start()
}
