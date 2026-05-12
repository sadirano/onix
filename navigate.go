package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

// readLine prints prompt and reads one line from stdin.
// Returns ("", false) when the user cancels with Ctrl+C or a stream error occurs.
//
// The prompt is written to stderr so callers that capture stdout via $() in
// bash or Tee-Object in PowerShell don't end up with the prompt text mixed
// into their captured value.
func readLine(prompt string) (string, bool) {
	fmt.Fprint(os.Stderr, prompt)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- result{line, err}
	}()

	select {
	case <-sig:
		fmt.Println()
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
func promptDestination(aliasName string) string {
	line, ok := readLine(fmt.Sprintf("Destination for %q: ", aliasName))
	if !ok {
		return ""
	}
	return line
}
