package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/sadirano/onix/internal/resolver"
	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/store"
)

// fastResolve is the hot-path implementation of `onix resolve <name>`.
// It uses the shared resolver which combines fast byte-scanning with
// a slow-path fallback. Side effects like directory creation are
// handled here at the command layer.
func fastResolve(home, name string, noPrompt bool, stdout, stderr io.Writer, stdin io.Reader) error {
	var prompter func(string) string
	var selector func([]string) string
	var segPrompter resolver.SegmentPrompter

	var multiSelector func(string, []string) string

	if !noPrompt {
		prompter = func(name string) string {
			return promptDestination(name, stderr, stdin)
		}
		selector = func(options []string) string {
			return promptSelection(options, stderr, stdin)
		}
		segPrompter = func(segmentName, inlineValue, aliasBase, aliasName string) (*segments.ContextDef, error) {
			return promptSegmentDefinition(home, segmentName, inlineValue, stderr, stdin, aliasBase, aliasName)
		}
		multiSelector = func(alias string, paths []string) string {
			return promptMultiTargetPath(alias, paths, stderr, stdin)
		}
	}
	p, err := resolver.Resolve(home, name, prompter, selector, segPrompter, multiSelector)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", p, err)
	}

	// Record usage for frecency ranking.
	_ = store.RecordUsage(home, name)

	fmt.Fprintln(stdout, p)
	return nil
}

// fastListNames prints alias names from aliases.toml, one per line, in
// frecency order (with alphabetical fallback). Used by the PowerShell
// tab-completer ($onixAliasCompleter) which fires every Tab press.
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

	scores := store.GetFrecencyScores(home)
	sort.Slice(names, func(i, j int) bool {
		sI := scores[strings.ToLower(names[i])]
		sJ := scores[strings.ToLower(names[j])]
		if sI != sJ {
			return sI > sJ
		}
		return names[i] < names[j]
	})

	for _, n := range names {
		fmt.Fprintln(stdout, n)
	}
	return nil
}
