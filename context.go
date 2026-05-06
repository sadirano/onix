package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	case "alias":
		sb.WriteString("path=" + cc.Path + "\n")
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
		case "path":
			cc.Path = v
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
	case "alias":
		fmt.Printf("path=%s\n", cc.Path)
	default:
		fmt.Printf("var=%s\n", cc.Var)
	}
	if cc.Template != "" {
		fmt.Printf("template=%s\n", cc.Template)
	}
}

// contextVarName returns the identifier to use as a named placeholder in
// templates: the var name for env source, the file path for file source, and
// the cmd string for cmd source. Returns "" when no meaningful name exists.
func contextVarName(cc config.ContextConfig) string {
	switch cc.Source {
	case "file":
		return cc.File
	case "cmd":
		return cc.Cmd
	default:
		return cc.Var
	}
}

// applyContextTemplate substitutes placeholders in template with value.
// Supports both {value} (generic) and {varName} (the configured var/cmd/file name).
// Leading/trailing path separators are stripped so the result can be safely
// joined with filepath.Join. When template is empty, value is returned as-is.
func applyContextTemplate(template, varName, value string) string {
	if template == "" {
		return value
	}
	result := strings.ReplaceAll(template, "{value}", value)
	if varName != "" {
		result = strings.ReplaceAll(result, "{"+varName+"}", value)
	}
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
	case "alias":
		return contextFromAlias(cc.Path)
	default: // "env"
		return contextFromEnv(cc.Var)
	}
}

// applySegment resolves a single @ segment using its context config, returning
// the path part to append. Returns an error when no context config exists —
// callers must ensure the config is present before calling (e.g. by prompting).
func applySegment(seg string, cfg *config.Config, debugEnabled bool) (string, error) {
	cc, ok := loadAliasContextConfig(seg)
	if !ok {
		return "", fmt.Errorf("segment %q: no context configured", seg)
	}
	if cc.Source == "alias" {
		part := strings.Trim(cc.Path, "/\\")
		if debugEnabled {
			fmt.Printf("[ONIX] segment %q → alias=%q\n", seg, part)
		}
		return part, nil
	}
	val, err := resolveContextConfig(cc)
	if err != nil {
		return "", fmt.Errorf("segment %q: %w", seg, err)
	}
	part := applyContextTemplate(cc.Template, contextVarName(cc), val)
	if debugEnabled {
		fmt.Printf("[ONIX] segment %q → template=%q value=%q → %q\n", seg, cc.Template, val, part)
	}
	return part, nil
}

func contextFromAlias(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("[context] alias path is empty")
	}
	return path, nil
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

// walkSegments resolves each @ segment against target right-to-left, returning
// the final target path. When a segment has no context config the user is
// prompted to configure one, which is then saved for future invocations.
func walkSegments(segments []string, target string, cfg *config.Config, debugEnabled bool) string {
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if _, ok := loadAliasContextConfig(seg); !ok {
			cc, ok := promptContextConfig(seg)
			if !ok {
				os.Exit(exitNotFound)
			}
			if err := writeAliasContextConfig(seg, cc); err != nil {
				fatalCode(exitErr, "save context for %q: %v", seg, err)
			}
		}
		part, err := applySegment(seg, cfg, debugEnabled)
		if err != nil {
			fatalCode(exitErr, "%v", err)
		}
		target = filepath.Join(target, part)
	}
	return target
}

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
