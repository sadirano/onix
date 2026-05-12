package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveHome returns the absolute path to the onix config directory.
// Precedence: explicit override > ONIX_HOME env (handled by kong) > ~/.onix.
// We don't create the directory here — that's `onix init`'s job.
func resolveHome(override string) (string, error) {
	if v := strings.TrimSpace(override); v != "" {
		abs, err := filepath.Abs(expandTilde(v))
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

// aliasesPath returns ~/.onix/aliases.toml.
func aliasesPath(home string) string {
	return filepath.Join(home, "aliases.toml")
}

// shellPath returns ~/.onix/shell/onix.ps1 (the PowerShell integration snippet).
func shellPath(home string) string {
	return filepath.Join(home, "shell", "onix.ps1")
}

// bashShellPath returns ~/.onix/shell/onix.sh (the Bash/Zsh integration snippet).
func bashShellPath(home string) string {
	return filepath.Join(home, "shell", "onix.sh")
}

// configPath returns ~/.onix/config.toml — the file that holds [[actions]]
// and (eventually) other user-tunable settings. Aliases live separately in
// aliases.toml so the hot path doesn't have to skip past action declarations.
func configPath(home string) string {
	return filepath.Join(home, "config.toml")
}

// expandTilde expands a leading ~ to the user home directory.
// Pure cosmetic helper — only the first character is examined.
func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return home + p[1:]
}
