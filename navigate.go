package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sadirano/onix/internal/segments"
)

// readLine prints prompt and reads one line from reader.
// Returns ("", false) when the user cancels with Ctrl+C or a stream error occurs.
//
// The prompt is written to stderr so callers that capture stdout via $() in
// bash or Tee-Object in PowerShell don't end up with the prompt text mixed
// into their captured value.
func readLine(prompt string, stderr io.Writer, reader *bufio.Reader) (string, bool) {
	fmt.Fprint(stderr, prompt)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- result{line, err}
	}()

	select {
	case <-sig:
		fmt.Fprintln(stderr)
		os.Exit(1)
		return "", false
	case r := <-ch:
		if r.err != nil {
			return "", false
		}
		return strings.TrimSpace(r.line), true
	}
}

// promptDestination asks the user for a target path for an unknown alias.
// Returns "" if the user cancels (Ctrl+C or empty input).
func promptDestination(aliasName string, stderr io.Writer, reader *bufio.Reader) string {
	header := fmt.Sprintf("Destination for %q (Tab to edit)", aliasName)

	if fzf, err := exec.LookPath("fzf"); err == nil {
		query := ""
		for {
			cmd := execCommand(fzf, "--header", header, "--reverse", "--height", "20%", "--print-query", "--query", query, "--expect=tab")
			cmd.Stderr = stderr
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stdin = strings.NewReader("")
			err := cmd.Run()

			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); !ok || (exitErr.ExitCode() != 1 && exitErr.ExitCode() != 0) {
					return ""
				}
			}

			lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			if len(lines) < 2 {
				return ""
			}

			key := lines[0]
			currentQuery := lines[1]
			selection := ""
			if len(lines) > 2 {
				selection = lines[2]
			}

			if key == "tab" {
				if selection != "" {
					query = selection
				} else {
					query = currentQuery
				}
				continue
			}

			if selection != "" {
				return selection
			}
			return currentQuery
		}
	}

	line, ok := readLine(header+": ", stderr, reader)
	if !ok {
		return ""
	}
	return line
}

// promptSegmentDefinition asks the user to define a [[contexts]] entry for
// a segment that wasn't found in segments.toml by opening their editor with
// a template and instructions.
func promptSegmentDefinition(home, segmentName, inlineValue string, stderr io.Writer, reader *bufio.Reader, aliasBase, aliasName string) (*segments.ContextDef, error) {
	ed := resolveEditor()
	if ed == "" {
		return nil, fmt.Errorf("no editor found: set $EDITOR or ensure one of nvim, vim, code, nano, notepad is on PATH")
	}

	filePath := segments.CentralPath(home, aliasName)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(filePath), err)
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}

	var sb strings.Builder
	sb.WriteString("\n[[contexts]]\n")
	sb.WriteString("segment = \"" + segmentName + "\"\n")
	if inlineValue != "" {
		sb.WriteString("# (Current inline value: " + inlineValue + ")\n")
	}
	sb.WriteString("source-template = \"/" + "${" + segmentName + "}" + "\"\n")

	if _, err := f.WriteString(sb.String()); err != nil {
		f.Close()
		return nil, fmt.Errorf("write %s: %w", filePath, err)
	}
	f.Close()

	parts := strings.Fields(ed)
	binary := parts[0]
	args := parts[1:]

	lowerEd := strings.ToLower(binary)
	isVim := strings.Contains(lowerEd, "vim") || strings.Contains(lowerEd, "nvim")

	if strings.Contains(lowerEd, "code") || strings.Contains(lowerEd, "nano") || isVim {
		// Jump to the end of the file.
		args = append(args, "+9999")
	}
	args = append(args, filePath)
	cmd := execCommand(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor %s: %w", ed, err)
	}

	sf, err := segments.LoadSegmentsFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("load segments: %w", err)
	}

	cd, ok := segments.LookupContext(sf, segmentName)
	if !ok {
		return nil, nil // User might have deleted it
	}

	fmt.Fprintf(stderr, "Saved [[contexts]] segment = %q in %s\n", segmentName, filePath)
	return cd, nil
}


func promptMultiTargetPath(alias string, paths []string, stderr io.Writer, reader *bufio.Reader) string {
	header := fmt.Sprintf("Multiple paths for %q — pick one:", alias)

	if fzf, err := exec.LookPath("fzf"); err == nil {
		cmd := execCommand(fzf, "--header", header, "--reverse", "--height", "40%")
		cmd.Stderr = stderr
		cmd.Stdin = strings.NewReader(strings.Join(paths, "\n"))
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		return ""
	}

	fmt.Fprintf(stderr, "%s\n", header)
	for i, p := range paths {
		fmt.Fprintf(stderr, "  %d) %s\n", i+1, p)
	}
	line, ok := readLine(fmt.Sprintf("Select [1-%d] or press Enter to cancel: ", len(paths)), stderr, reader)
	if !ok || line == "" {
		return ""
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(paths) {
		return ""
	}
	return paths[idx-1]
}

