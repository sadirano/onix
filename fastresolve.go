package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/sadirano/onix/internal/resolver"
	"github.com/sadirano/onix/internal/store"
)

// fastResolve is the hot-path implementation of `onix resolve <name>`.
// It uses the shared resolver which combines fast byte-scanning with
// a slow-path fallback. Side effects like directory creation are
// handled here at the command layer.
func fastResolve(home, name string) error {
	p, err := resolver.Resolve(home, name, promptDestination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", p, err)
	}
	fmt.Println(p)
	return nil
}

// fastListNames prints alias names from aliases.toml, one per line, in
// sorted order. Used by the PowerShell tab-completer ($onixAliasCompleter)
// which fires every Tab press.
func fastListNames(home string) error {
	data, err := os.ReadFile(store.AliasesPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, 32)
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := trimLine(raw)
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
		fmt.Println(n)
	}
	return nil
}

// trimLine trims a single line of leading/trailing spaces, tabs, and the
// trailing CR.
func trimLine(line []byte) []byte {
	for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		line = line[1:]
	}
	for len(line) > 0 {
		c := line[len(line)-1]
		if c == ' ' || c == '\t' || c == '\r' {
			line = line[:len(line)-1]
			continue
		}
		break
	}
	return line
}
