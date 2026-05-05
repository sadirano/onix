package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// resolveContext returns the active context string using the source configured
// in [context]. Returns ("", nil) when no context section is present —
// callers should omit the context layer from the path in that case.
func resolveContext(cfg *config.Config) (string, error) {
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
