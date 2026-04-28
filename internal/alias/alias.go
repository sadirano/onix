package alias

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultDirName  = ".omni"
	defaultFileName = ".env"
)

// FilePath returns the active alias file path.
// Precedence: ONIX_ENV > ONIX_ALIAS_FILE > OMNI_ENV > ~/.omni/.env
func FilePath() string {
	// OMNI_* env vars are kept for backwards compatibility with the predecessor tool "omni".
	for _, env := range []string{"ONIX_ENV", "ONIX_ALIAS_FILE", "OMNI_ENV", "OMNI_ALIAS_FILE"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(defaultDirName, defaultFileName)
	}
	return filepath.Join(home, defaultDirName, defaultFileName)
}

// Load reads all aliases from the active alias file.
func Load() (map[string]string, error) {
	content, err := os.ReadFile(FilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")

	aliases := make(map[string]string)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			aliases[key] = value
		}
	}
	return aliases, nil
}

// Register upserts an alias entry in the active alias file.
func Register(name, destination string) error {
	file := FilePath()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}

	content, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var lines []string
	if len(content) > 0 {
		text := strings.ReplaceAll(string(content), "\r\n", "\n")
		text = strings.TrimPrefix(text, "\ufeff")
		lines = strings.Split(text, "\n")
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
	}

	entry := fmt.Sprintf("%s=%s", name, destination)
	replaced := false
	out := make([]string, 0, len(lines)+1)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if i == len(lines)-1 {
				continue
			}
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			out = append(out, entry)
			replaced = true
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		out = append(out, entry)
	}

	data := strings.Join(out, "\r\n")
	if !strings.HasSuffix(data, "\r\n") {
		data += "\r\n"
	}
	return os.WriteFile(file, []byte(data), 0o644)
}

// OpenInEditor opens the active alias file in the given editor.
func OpenInEditor(editor string) error {
	f := FilePath()
	cmd := exec.Command(editor, f)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open editor: %w", err)
	}
	_ = cmd.Wait()
	return nil
}

// ApplyEnvOverride propagates aliasFile into ONIX_ALIAS_FILE so that child
// processes (module binaries) inherit the same alias file path as the parent.
// It is a no-op when aliasFile is empty or any alias-file env var is already set,
// so an explicit env override always wins over the config file setting.
// OMNI_* env vars are kept for backwards compatibility with the predecessor tool "omni".
func ApplyEnvOverride(aliasFile string) {
	if strings.TrimSpace(aliasFile) == "" {
		return
	}
	for _, env := range []string{"ONIX_ENV", "ONIX_ALIAS_FILE", "OMNI_ENV", "OMNI_ALIAS_FILE"} {
		if strings.TrimSpace(os.Getenv(env)) != "" {
			return
		}
	}
	if err := os.Setenv("ONIX_ALIAS_FILE", aliasFile); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not set ONIX_ALIAS_FILE: %v\n", err)
	}
}

// Resolve returns the absolute path for the given alias or raw path.
// Unlike omni, it never prompts — unknown aliases are an error.
func Resolve(input string, debug bool) (string, error) {
	// Accept a raw path directly.
	if _, err := os.Stat(input); err == nil {
		abs, err := filepath.Abs(input)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", input, err)
		}
		return abs, nil
	}

	aliases, err := Load()
	if err != nil {
		return "", err
	}

	for k, v := range aliases {
		if strings.EqualFold(k, input) {
			if debug {
				fmt.Printf("[ONIX] resolved %q -> %q\n", input, v)
			}
			return v, nil
		}
	}

	return "", fmt.Errorf("unknown alias %q — register it with: onix -a %s -d <path>", input, input)
}
