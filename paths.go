package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sadirano/onix/internal/store"
)

// resolveHome returns the absolute path to the onix config directory.
// Precedence: explicit override (the $ONIX_HOME value passed by main) > ~/.onix.
// We don't create the directory here — that's `onix init`'s job.
func resolveHome(override string) (string, error) {
	if v := strings.TrimSpace(override); v != "" {
		abs, err := filepath.Abs(store.ExpandTilde(v))
		if err != nil {
			return "", fmt.Errorf("resolve config dir %q: %w", v, err)
		}
		return abs, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Windows fallback when USERPROFILE/HOMEDRIVE aren't set under some
		// service accounts. Empty is fatal — we'd silently land in CWD otherwise.
		if h := os.Getenv("USERPROFILE"); h != "" {
			home = h
		} else if d, p := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"); d != "" && p != "" {
			home = d + p
		} else {
			return "", fmt.Errorf("cannot determine home directory")
		}
	}
	return filepath.Join(home, ".onix"), nil
}

// pwshBin returns the name of the PowerShell executable to use.
// It prefers 'pwsh' (PowerShell Core) but falls back to 'powershell.exe'
// (Windows PowerShell) if pwsh isn't on PATH.
func pwshBin() string {
	if _, err := lookPath("pwsh"); err == nil {
		return "pwsh"
	}
	if runtime.GOOS == "windows" {
		return "powershell.exe"
	}
	return "pwsh" // best guess on non-windows
}
