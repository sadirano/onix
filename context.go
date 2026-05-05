package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// aliasContextPath returns the path of the per-alias pinned context file.
func aliasContextPath(alias string) string {
	return filepath.Join(config.Dir(), "contexts", alias)
}

// setAliasContext writes value as the pinned context for alias.
func setAliasContext(alias, value string) error {
	dir := filepath.Join(config.Dir(), "contexts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create contexts dir: %w", err)
	}
	return os.WriteFile(aliasContextPath(alias), []byte(value+"\n"), 0o644)
}

// getAliasContext reads the pinned context for alias. Returns ("", false) when
// no context has been pinned.
func getAliasContext(alias string) (string, bool) {
	b, err := os.ReadFile(aliasContextPath(alias))
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", false
	}
	return v, true
}

// clearAliasContext removes the pinned context for alias.
func clearAliasContext(alias string) error {
	err := os.Remove(aliasContextPath(alias))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// resolveContext returns the active context string for aliasName.
// Per-alias pinned values (set via "onix ctx") take priority over the global
// [context] config. Returns ("", nil) when no context is configured or pinned —
// callers should omit the context layer from the path in that case.
func resolveContext(aliasName string, cfg *config.Config) (string, error) {
	if v, ok := getAliasContext(aliasName); ok {
		return v, nil
	}
	if !cfg.HasContext() {
		return "", nil
	}
	switch cfg.Context.Source {
	case "file":
		return contextFromFile(cfg.Context.File)
	case "cmd":
		return contextFromCmd(cfg.Context.Cmd)
	default: // "env"
		return contextFromEnv(cfg.Context.Var)
	}
}

func contextFromEnv(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("[context] var not configured")
	}
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", fmt.Errorf("context env var %q is not set", name)
	}
	return v, nil
}

func contextFromFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("[context] file not configured")
	}
	b, err := os.ReadFile(expandTilde(path))
	if err != nil {
		return "", fmt.Errorf("read context file %q: %w", path, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("context file %q is empty", path)
	}
	return v, nil
}

func contextFromCmd(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("[context] cmd not configured")
	}
	out, err := exec.Command("cmd.exe", "/C", command).Output()
	if err != nil {
		return "", fmt.Errorf("context command %q: %w", command, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("context command %q produced no output", command)
	}
	return v, nil
}

// expandTilde expands a leading ~ to the user home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}
