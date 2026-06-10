package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/segments"
)

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

	// Route the editor to the real console. This function is reached from the
	// `o.cmd` flow, which runs `onix <alias> > .last 2>nul` — our stdout/stderr
	// are redirected, so a terminal editor inheriting them would see a non-tty
	// and fail to render. consoleIO falls back to the std handles when there's
	// no console.
	ttyIn, ttyOut, ttyClose := consoleIO()
	defer ttyClose()

	cmd := execCommand(binary, args...)
	cmd.Stdin = ttyIn
	cmd.Stdout = ttyOut
	cmd.Stderr = ttyOut
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
