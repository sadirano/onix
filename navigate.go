package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"

	"github.com/sadirano/onix/internal/segments"
)

// readLine prints prompt and reads one line from stdin.
// Returns ("", false) when the user cancels with Ctrl+C or a stream error occurs.
//
// The prompt is written to stderr so callers that capture stdout via $() in
// bash or Tee-Object in PowerShell don't end up with the prompt text mixed
// into their captured value.
func readLine(prompt string, stderr io.Writer, stdin io.Reader) (string, bool) {
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
		line, err := bufio.NewReader(stdin).ReadString('\n')
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
func promptDestination(aliasName string, stderr io.Writer, stdin io.Reader) string {
	line, ok := readLine(fmt.Sprintf("Destination for %q: ", aliasName), stderr, stdin)
	if !ok {
		return ""
	}
	return line
}

// promptSegmentDefinition asks the user to define a [[contexts]] entry for
// a segment that wasn't found in segments.toml. On success it persists the
// new context to disk and returns the saved ContextDef. Returns
// (nil, nil) if the user cancels via Ctrl+C or an empty answer.
//
// The save step is unconditional once the user supplies a valid source —
// the [Y/n] confirmation is "save / abort", not "save / skip".
//
// All prompts share a single bufio.Reader so each ReadString consumes
// exactly one line — using the package-level readLine helper per prompt
// creates a fresh reader each call, which would swallow buffered input.
func promptSegmentDefinition(home, segmentName, inlineValue string, stderr io.Writer, stdin io.Reader) (*segments.ContextDef, error) {
	fmt.Fprintf(stderr, "segment %q is not defined.\n", segmentName)
	if inlineValue != "" {
		fmt.Fprintf(stderr, "  (inline value: %s)\n", inlineValue)
	}
	fmt.Fprintln(stderr, "")

	reader := bufio.NewReader(stdin)
	read := func(prompt string) (string, bool) {
		fmt.Fprint(stderr, prompt)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", false
		}
		return strings.TrimSpace(line), true
	}

	kind, ok := pickSegmentSource(segmentName, stderr, reader)
	if !ok || kind == "" {
		return nil, nil
	}

	cd := &segments.ContextDef{Segment: segmentName}
	switch kind {
	case "template":
		v, ok := read("Template: ")
		if !ok || v == "" {
			return nil, nil
		}
		cd.SourceTemplate = v
	case "exec":
		v, ok := read("Exec (command + space-separated args): ")
		if !ok || v == "" {
			return nil, nil
		}
		cd.SourceExec = strings.Fields(v)
	case "file":
		v, ok := read("File path: ")
		if !ok || v == "" {
			return nil, nil
		}
		cd.SourceFile = v
	default:
		return nil, fmt.Errorf("segment prompt: unrecognised choice %q (expected template, exec, or file)", kind)
	}

	confirm, ok := read("Save to segments.toml? [Y/n] ")
	if !ok {
		return nil, nil
	}
	if confirm != "" && !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
		return nil, nil
	}

	sf, err := segments.LoadSegments(home)
	if err != nil {
		return nil, fmt.Errorf("segment prompt: %w", err)
	}
	sf.Contexts = append(sf.Contexts, *cd)
	if err := segments.SaveSegments(home, sf); err != nil {
		return nil, fmt.Errorf("segment prompt: save: %w", err)
	}
	fmt.Fprintf(stderr, "Saved [[contexts]] segment = %q\n", segmentName)
	return cd, nil
}

// pickSegmentSource asks the user to choose the source kind for a new
// segment context. Returns one of "template", "exec", "file", or "" on
// cancel. ok=false means the input stream failed (treat as cancel).
//
// When fzf is on PATH it is used as the picker; otherwise a numbered prompt
// reads from reader. The two paths produce the same set of return values so
// promptSegmentDefinition can switch on the result either way.
func pickSegmentSource(segmentName string, stderr io.Writer, reader *bufio.Reader) (string, bool) {
	options := []struct {
		kind  string
		label string
	}{
		{"template", "template  formatted path, e.g. /${" + segmentName + "}, /tickets/${" + segmentName + "}/notes"},
		{"exec", "exec      run a command, capture stdout"},
		{"file", "file      read a file's contents"},
	}

	if fzf, err := exec.LookPath("fzf"); err == nil {
		lines := make([]string, len(options))
		for i, o := range options {
			lines[i] = o.label
		}
		cmd := execCommand(fzf, "--header", "Pick a source for segment "+segmentName, "--reverse", "--height", "20%", "--no-sort")
		cmd.Stderr = stderr
		cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
		out, err := cmd.Output()
		if err != nil {
			return "", true
		}
		selected := strings.TrimSpace(string(out))
		for _, o := range options {
			if o.label == selected {
				return o.kind, true
			}
		}
		return "", true
	}

	fmt.Fprintln(stderr, "Pick a source:")
	for i, o := range options {
		fmt.Fprintf(stderr, "  [%d] %s\n", i+1, o.label)
	}
	fmt.Fprint(stderr, "> ")
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return "", true
	}
	for i, o := range options {
		if choice == fmt.Sprintf("%d", i+1) {
			return o.kind, true
		}
	}
	return choice, true
}

// promptSelection presents a list of options and returns the selected one.
// It auto-detects 'fzf' and falls back to a numeric list.
func promptSelection(options []string, stderr io.Writer, stdin io.Reader) string {
	if len(options) == 0 {
		return ""
	}

	// Try fzf first
	if fzf, err := exec.LookPath("fzf"); err == nil {
		cmd := execCommand(fzf, "--header", "Did you mean:", "--reverse", "--height", "20%")
		cmd.Stderr = stderr
		cmd.Stdin = strings.NewReader(strings.Join(options, "\n"))
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		// If fzf was cancelled (exit code 130) or errored, we just return ""
		// so the user can continue with the unknown alias error.
		return ""
	}

	// Fallback to numeric prompt
	fmt.Fprintln(stderr, "Did you mean:")
	for i, opt := range options {
		fmt.Fprintf(stderr, "  %d) %s\n", i+1, opt)
	}

	line, ok := readLine(fmt.Sprintf("Select [1-%d] or press Enter to cancel: ", len(options)), stderr, stdin)
	if !ok || line == "" {
		return ""
	}

	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(options) {
		return ""
	}

	return options[idx-1]
}
