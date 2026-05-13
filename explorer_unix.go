//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInExplorer opens the target path in the system file manager.
// On Linux we use xdg-open; on macOS we use open.
func openInExplorer(target string) error {
	bin := "xdg-open"
	if runtime.GOOS == "darwin" {
		bin = "open"
	}

	// We use Start() rather than Run() so onix doesn't block while the
	// explorer window is open.
	if err := exec.Command(bin, target).Start(); err != nil {
		return fmt.Errorf("%s %s: %w", bin, target, err)
	}
	return nil
}
