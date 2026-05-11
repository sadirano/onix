package opener

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Match represents a single search result with position information.
type Match struct {
	Path string
	Line int
	Col  int
	Text string
}

// IsBinaryFile reports whether path is binary by scanning the first 512 bytes
// for null bytes — the same heuristic git uses.
func IsBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false
	}
	return bytes.IndexByte(buf[:n], 0) >= 0
}

// OpenMixedFiles splits files into binary vs text, launches binary files with
// the OS default program, and opens text files together in the editor.
func OpenMixedFiles(editor string, files []string) error {
	var textFiles []string
	for _, f := range files {
		if IsBinaryFile(f) {
			if err := OpenFileWithDefault(f); err != nil {
				return fmt.Errorf("open default %s: %w", f, err)
			}
		} else {
			textFiles = append(textFiles, f)
		}
	}
	if len(textFiles) == 0 {
		return nil
	}
	return RunEditorCommand(editor, "", textFiles...)
}

// RunEditorCommand runs editor with the given args in dir (empty dir = inherit CWD).
func RunEditorCommand(editor, dir string, args ...string) error {
	cmd := exec.Command(editor, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open editor: %w", err)
	}
	return nil
}

// OpenSearchMatches opens search matches in editor, using line/column
// navigation for VS Code and vim/nvim, falling back to paths-only.
func OpenSearchMatches(editor, dir string, matches []Match) error {
	base := EditorBase(editor)
	if base == "code" || base == "code-insiders" {
		for i, m := range matches {
			gotoArg := fmt.Sprintf("%s:%d:%d", m.Path, m.Line, m.Col)
			var err error
			if i == 0 {
				err = RunEditorCommand(editor, dir, "-g", gotoArg)
			} else {
				err = RunEditorCommand(editor, dir, "-r", "-g", gotoArg)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	if base == "nvim" || base == "vim" {
		if len(matches) == 1 {
			return RunEditorCommand(editor, dir, fmt.Sprintf("+%d", matches[0].Line), matches[0].Path)
		}
		tmp, err := os.CreateTemp("", "onix-*.qf")
		if err != nil {
			return fmt.Errorf("create temp quickfix file: %w", err)
		}
		defer os.Remove(tmp.Name())
		for _, m := range matches {
			fmt.Fprintf(tmp, "%s:%d:%d:%s\n", m.Path, m.Line, m.Col, m.Text)
		}
		_ = tmp.Close()
		return RunEditorCommand(editor, dir, "-q", tmp.Name())
	}
	paths := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		if _, ok := seen[m.Path]; ok {
			continue
		}
		seen[m.Path] = struct{}{}
		paths = append(paths, m.Path)
	}
	return RunEditorCommand(editor, dir, paths...)
}

// OpenSearchMatchesMixed is like OpenSearchMatches but opens binary matched
// files (e.g. PDFs found by rga) with the OS default program instead of the editor.
func OpenSearchMatchesMixed(editor, dir string, matches []Match) error {
	var textMatches []Match
	seenBinary := map[string]struct{}{}
	for _, m := range matches {
		absPath := filepath.Join(dir, m.Path)
		if IsBinaryFile(absPath) {
			if _, seen := seenBinary[m.Path]; !seen {
				seenBinary[m.Path] = struct{}{}
				if err := OpenFileWithDefault(absPath); err != nil {
					return fmt.Errorf("open default %s: %w", m.Path, err)
				}
			}
		} else {
			textMatches = append(textMatches, m)
		}
	}
	if len(textMatches) == 0 {
		return nil
	}
	return OpenSearchMatches(editor, dir, textMatches)
}

// EditorBase returns the lowercase base name of editor without extension.
// Backslashes are normalised to forward slashes so Windows paths work on any OS.
func EditorBase(editor string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(editor), `\`, `/`)
	base := strings.ToLower(filepath.Base(normalized))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
