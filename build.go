package main

import (
	"fmt"
	"os"
	"path/filepath"
	rdebug "runtime/debug"
	"strings"
	"time"

	"github.com/sadirano/onix/internal/config"
)

// buildVersion is injected at link time via -ldflags "-X main.buildVersion=<ver>".
// Falls back to "dev" and then to VCS revision from build info.
var buildVersion = "dev"

func execBasename() string {
	base := filepath.Base(os.Args[0])
	return strings.ToLower(strings.TrimSuffix(base, ".exe"))
}

// resolveOnixBinaryInfo returns the path to the running onix.exe.
// If the current executable is not named onix.exe (e.g. a shortcut copy),
// it falls back to ~/.onix/onix.exe.
func resolveOnixBinaryInfo() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe = strings.TrimSpace(exe)
	if strings.EqualFold(filepath.Base(exe), "onix.exe") {
		return exe, nil
	}
	defaultOnix := filepath.Join(config.Dir(), "onix.exe")
	if _, err := os.Stat(defaultOnix); err == nil {
		return defaultOnix, nil
	}
	return exe, nil
}

func resolvedBuildVersion() string {
	if v := strings.TrimSpace(buildVersion); v != "" && v != "dev" {
		return v
	}
	if bi, ok := rdebug.ReadBuildInfo(); ok && bi != nil {
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return bi.Main.Version
		}
		for _, setting := range bi.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				if len(setting.Value) > 12 {
					return setting.Value[:12]
				}
				return setting.Value
			}
		}
	}
	return "dev"
}

func printBuildDebugInfo() {
	onixPath, infoErr := resolveOnixBinaryInfo()
	version := resolvedBuildVersion()
	modifiedAt := "unknown"
	if infoErr == nil {
		if fi, err := os.Stat(onixPath); err == nil {
			modifiedAt = fi.ModTime().Format(time.RFC3339)
		}
	}
	if onixPath == "" {
		onixPath = "<unknown>"
	}
	fmt.Fprintf(os.Stderr, "[ONIX] build_version=%s onix_exe=%s modified_at=%s\n", version, onixPath, modifiedAt)
}
