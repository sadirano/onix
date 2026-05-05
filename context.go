package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
)

// aliasContextPath returns the path of the per-segment context config file.
func aliasContextPath(alias string) string {
	return filepath.Join(config.Dir(), "contexts", alias)
}

// writeAliasContextConfig serialises cc into the per-segment context file using
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
	if cc.Template != "" {
		sb.WriteString("template=" + cc.Template + "\n")
	}
	return os.WriteFile(aliasContextPath(alias), []byte(sb.String()), 0o644)
}

// loadAliasContextConfig reads and parses the per-segment context config.
// Returns (zero, false) when no config exists for the segment.
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
			cc.Cmd = v // preserve spacing — cmd values may contain = signs
		case "template":
			cc.Template = v
		}
	}
	if cc.Source == "" {
		return config.ContextConfig{}, false
	}
	return cc, true
}

// clearAliasContext removes the per-segment context config.
func clearAliasContext(alias string) error {
	err := os.Remove(aliasContextPath(alias))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// printAliasContextConfig prints the context config for a segment to stdout.
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
	if cc.Template != "" {
		fmt.Printf("template=%s\n", cc.Template)
	}
}

// applyContextTemplate substitutes {value} in template with value.
// Leading/trailing path separators are stripped so the result can be safely
// joined with filepath.Join. When template is empty, value is returned as-is.
func applyContextTemplate(template, value string) string {
	if template == "" {
		return value
	}
	result := strings.ReplaceAll(template, "{value}", value)
	return strings.Trim(result, "/\\")
}

// resolveContext returns the active context string for a segment.
// Checks the per-segment config file first, then falls back to the global
// [context] section in config.toml.
func resolveContext(segmentName string, cfg *config.Config) (string, error) {
	if cc, ok := loadAliasContextConfig(segmentName); ok {
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

// applySegment resolves a single @ segment against target, returning the new
// path segment to append. If the segment has a context config, its template is
// applied with the resolved value. Otherwise the subdir registry is consulted.
func applySegment(seg, target string, cfg *config.Config, debugEnabled bool) (string, error) {
	if cc, ok := loadAliasContextConfig(seg); ok {
		val, err := resolveContextConfig(cc)
		if err != nil {
			return "", fmt.Errorf("segment %q: %w", seg, err)
		}
		part := applyContextTemplate(cc.Template, val)
		if debugEnabled {
			fmt.Printf("[ONIX] segment %q → template=%q value=%q → %q\n", seg, cc.Template, val, part)
		}
		return part, nil
	}
	resolved := alias.ResolveSubdir(seg, target)
	if debugEnabled {
		fmt.Printf("[ONIX] segment %q → subdir=%q\n", seg, resolved)
	}
	return resolved, nil
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
