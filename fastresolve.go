package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/sadirano/onix/internal/resolver"
	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/store"
)

// fastResolve is the hot-path implementation of `onix resolve <name>`.
// It uses the shared resolver which combines fast byte-scanning with
// a slow-path fallback. Side effects like directory creation are
// handled here at the command layer.
func fastResolve(home, name string, stdout, stderr io.Writer, stdin io.Reader, t *timer) error {
	var segPrompter resolver.SegmentPrompter

	// Use environment for NoPrompt
	noPrompt := os.Getenv("ONIX_NO_PROMPT") == "1"

	if !noPrompt {
		reader := bufio.NewReader(stdin)
		segPrompter = func(segmentName, inlineValue, aliasBase, aliasName string) (*segments.ContextDef, error) {
			return promptSegmentDefinition(home, segmentName, inlineValue, stderr, reader, aliasBase, aliasName)
		}
	}
	p, err := resolver.Resolve(home, name, segPrompter, t)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", p, err)
	}

	fmt.Fprintln(stdout, p)
	return nil
}

// fastListNames prints alias names from aliases.toml, one per line, in
// alphabetical order. Used by the PowerShell tab-completer
// ($onixAliasCompleter) which fires every Tab press.
func fastListNames(home string, stdout io.Writer) error {
	data, err := os.ReadFile(store.AliasesPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, 32)
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := resolver.TrimLine(raw)
		if len(line) == 0 || line[0] != '[' {
			continue
		}
		end := bytes.IndexByte(line, ']')
		if end <= 1 {
			continue
		}
		names = append(names, string(line[1:end]))
	}
	sort.Strings(names)

	for _, n := range names {
		fmt.Fprintln(stdout, n)
	}
	return nil
}
