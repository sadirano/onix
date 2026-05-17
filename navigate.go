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
