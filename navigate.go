package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/visual"
)

// isUNCPath reports whether path is a UNC network path (\\server\share\...).
func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`)
}

// openShellAt opens an interactive cmd.exe at dir. For UNC paths it uses
// pushd to map the share to a temporary drive letter, since cmd.exe cannot
// cd to a UNC path directly.
func openShellAt(dir string) error {
	var cmd *exec.Cmd
	if isUNCPath(dir) {
		cmd = exec.Command("cmd.exe", "/K", fmt.Sprintf(`pushd "%s"`, dir))
	} else {
		cmd = exec.Command("cmd.exe", "/K")
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}

// selectDestination opens an fzf directory picker seeded with aliasName as
// the initial query. Falls back to a plain text prompt if fzf is unavailable
// or the user presses Esc.
func selectDestination(aliasName string) string {
	if selected := fzfPickDir(aliasName); selected != "" {
		return selected
	}
	return promptDestination(aliasName)
}

func fzfPickDir(query string) string {
	if _, err := exec.LookPath("fzf"); err != nil {
		return ""
	}

	var input *bytes.Buffer

	// Prefer es (Everything) — results are instant.
	if _, err := exec.LookPath("es"); err == nil {
		if out, err := exec.Command("es", "-ad", query).Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
			input = bytes.NewBuffer(out)
		}
	}

	// Fallback: walk drive roots up to 3 levels deep.
	if input == nil {
		var buf bytes.Buffer
		for _, drive := range availableDrives() {
			filepath.Walk(drive, func(path string, info os.FileInfo, err error) error {
				if err != nil || !info.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(drive, path)
				if rel == "." {
					return nil
				}
				if strings.Count(rel, string(filepath.Separator)) >= 3 {
					return filepath.SkipDir
				}
				buf.WriteString(path + "\n")
				return nil
			})
		}
		input = &buf
	}

	fzfArgs := []string{
		"--prompt", activeVisuals.FZF.Destination.Prompt,
		"--query", query,
		"--height", "100%",
	}
	fzfArgs = visual.AppendLayoutArg(fzfArgs, activeVisuals.FZF.Destination.Layout)
	if header := strings.TrimSpace(activeVisuals.FZF.Destination.Header); header != "" {
		fzfArgs = append(fzfArgs, "--header", header)
	}
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = input
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		return "" // Esc pressed or fzf error
	}
	return strings.TrimSpace(string(out))
}

func availableDrives() []string {
	var drives []string
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + `:\`
		if _, err := os.Stat(drive); err == nil {
			drives = append(drives, drive)
		}
	}
	return drives
}

func promptDestination(aliasName string) string {
	fmt.Printf("Destination for %q: ", aliasName)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
