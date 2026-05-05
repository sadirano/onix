package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// aliasContextPath returns the path of the per-alias context config file.
func aliasContextPath(alias string) string {
	return filepath.Join(config.Dir(), "contexts", alias)
}

// writeAliasContextConfig serialises cc into the per-alias context file using
// the same key=value format as the alias file.
func writeAliasContextConfig(alias string, cc config.ContextConfig) error {
	dir := filepath.Join(config.Dir(), "contexts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create contexts dir: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("source=" + cc.Source + "\n")
	switch cc.Source {
	case "file":
		sb.WriteString("file=" + cc.File + "\n")
	case "cmd":
		sb.WriteString("cmd=" + cc.Cmd + "\n")
	default:
		sb.WriteString("var=" + cc.Var + "\n")
	}
	return os.WriteFile(aliasContextPath(alias), []byte(sb.String()), 0o644)
}

// loadAliasContextConfig reads and parses the per-alias context config.
// Returns (zero, false) when no config exists for the alias.
func loadAliasContextConfig(alias string) (config.ContextConfig, bool) {
	b, err := os.ReadFile(aliasContextPath(alias))
	if err != nil {
		return config.ContextConfig{}, false
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	cc := config.ContextConfig{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "source":
			cc.Source = strings.TrimSpace(v)
		case "var":
			cc.Var = strings.TrimSpace(v)
		case "file":
			cc.File = strings.TrimSpace(v)
		case "cmd":
			cc.Cmd = strings.TrimSpace(v)
		}
	}
	if cc.Source == "" {
		return config.ContextConfig{}, false
	}
	return cc, true
}

// clearAliasContext removes the per-alias context config.
func clearAliasContext(alias string) error {
	err := os.Remove(aliasContextPath(alias))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// printAliasContextConfig prints the context config for alias to stdout.
func printAliasContextConfig(alias string) {
	cc, ok := loadAliasContextConfig(alias)
	if !ok {
		fmt.Printf("no context configured for %q\n", alias)
		return
	}
	fmt.Printf("source=%s\n", cc.Source)
	switch cc.Source {
	case "file":
		fmt.Printf("file=%s\n", cc.File)
	case "cmd":
		fmt.Printf("cmd=%s\n", cc.Cmd)
	default:
		fmt.Printf("var=%s\n", cc.Var)
	}
}

// resolveContext returns the active context string for aliasName.
// Per-alias config (set via "onix ctx") takes priority over the global
// [context] config. Returns ("", nil) when no context is configured —
// callers should omit the context layer from the path in that case.
func resolveContext(aliasName string, cfg *config.Config) (string, error) {
	if cc, ok := loadAliasContextConfig(aliasName); ok {
		return resolveContextConfig(cc)
	}
	if !cfg.HasContext() {
		return "", nil
	}
	return resolveContextConfig(cfg.Context)
}

// resolveContextConfig resolves a context value from a ContextConfig.
func resolveContextConfig(cc config.ContextConfig) (string, error) {
	switch cc.Source {
	case "file":
		return contextFromFile(cc.File)
	case "cmd":
		return contextFromCmd(cc.Cmd)
	default: // "env"
		return contextFromEnv(cc.Var)
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
