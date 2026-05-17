package segments

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// execCommand is the package-level indirection for source-exec evaluation
// so tests can inject a fake without spawning a real process.
var execCommand = exec.Command

// LookupFunc is the variable-resolution callback shared by all source
// evaluators. Returns (value, true) for a bound name; (_, false) is an
// unresolved-variable error inside ExpandTemplate.
type LookupFunc func(name string) (string, bool)

// EvalTemplateSource expands tmpl with lookup. The result is the raw
// fragment — callers must run GuardFragment before joining it onto the
// alias target.
func EvalTemplateSource(tmpl string, lookup LookupFunc) (string, error) {
	return ExpandTemplate(tmpl, "source-template", lookup)
}

// EvalExecSource expands ${VAR} inside each arg, runs the command in cwd
// (typically the alias's base path), and returns the trimmed stdout.
// Non-zero exit is an error that mentions stderr.
//
// cwd may be empty, in which case the child process inherits the current
// working directory.
func EvalExecSource(args []string, cwd string, lookup LookupFunc) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("source-exec: empty argument list")
	}
	expanded := make([]string, len(args))
	for i, a := range args {
		where := fmt.Sprintf("source-exec[%d]", i)
		v, err := ExpandTemplate(a, where, lookup)
		if err != nil {
			return "", err
		}
		expanded[i] = v
	}
	cmd := execCommand(expanded[0], expanded[1:]...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("source-exec %v: %w: %s", expanded, err, stderrStr)
		}
		return "", fmt.Errorf("source-exec %v: %w", expanded, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// EvalFileSource resolves rawPath (after ${VAR} expansion), reads the
// referenced file, and returns its trimmed contents.
//
// Path prefixes:
//   - `@home/...`  → resolved relative to home (the onix home directory).
//   - `@alias/...` → resolved relative to aliasBase.
//   - `~/...`      → user home (via os.UserHomeDir).
//   - absolute     → used as-is (after the above prefix handling).
//   - anything else → returned as-is for filepath.IsAbs / cleanup.
//     Non-absolute, non-prefixed paths are an error to avoid silent
//     ambiguity about which root they would resolve against.
func EvalFileSource(rawPath, home, aliasBase string, lookup LookupFunc) (string, error) {
	expanded, err := ExpandTemplate(rawPath, "source-file", lookup)
	if err != nil {
		return "", err
	}
	resolved, err := resolveFileSourcePath(expanded, home, aliasBase)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("source-file %s: %w", resolved, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func resolveFileSourcePath(p, home, aliasBase string) (string, error) {
	switch {
	case strings.HasPrefix(p, "@home/") || strings.HasPrefix(p, "@home\\"):
		if home == "" {
			return "", fmt.Errorf("source-file: @home prefix used but onix home is unset")
		}
		return filepath.Join(home, filepath.FromSlash(p[len("@home/"):])), nil
	case strings.HasPrefix(p, "@alias/") || strings.HasPrefix(p, "@alias\\"):
		if aliasBase == "" {
			return "", fmt.Errorf("source-file: @alias prefix used but alias base is empty")
		}
		return filepath.Join(aliasBase, filepath.FromSlash(p[len("@alias/"):])), nil
	case strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\"):
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("source-file: expand ~: %w", err)
		}
		return filepath.Join(userHome, filepath.FromSlash(p[2:])), nil
	case filepath.IsAbs(p):
		return filepath.FromSlash(p), nil
	default:
		return "", fmt.Errorf("source-file %q: must be absolute or use @home/, @alias/, or ~/ prefix", p)
	}
}
