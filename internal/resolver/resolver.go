package resolver

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/store"
)

// ErrCancelled is returned by Resolve when the user cancels the interactive
// destination prompt for an unknown alias.
var ErrCancelled = errors.New("prompt cancelled")

// Resolve finds the absolute path for an alias.
//
// It first attempts a fast byte-scan of aliases.toml to bypass full TOML
// parsing. If the alias isn't found or contains segments (@), it falls back
// to the slow path using the Store and SegmentsFile.
//
// If the alias is still unknown and a prompter is provided, it calls the
// prompter to get a target path, registers it, and returns the new path.
//
// Resolve does NOT create directories on disk. It returns a host-native path
// (using filepath.FromSlash).
func Resolve(home, name string, prompter func(string) string) (string, error) {
	if strings.Contains(name, "@") {
		return resolveSegmented(home, name)
	}

	data, err := os.ReadFile(store.AliasesPath(home))
	if err == nil {
		target := strings.ToLower(strings.TrimSpace(name))
		if p, ok := ScanForAlias(data, target); ok {
			return filepath.FromSlash(p), nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// Fallback to slow path.
	s, err := store.LoadStore(home)
	if err != nil {
		return "", err
	}
	a, ok := s.Lookup(name)
	if !ok {
		if err := store.ValidateAliasName(name); err != nil {
			return "", err
		}
		if prompter == nil {
			return "", fmt.Errorf("unknown alias %q", name)
		}
		dest := prompter(name)
		if dest == "" {
			return "", ErrCancelled
		}
		abs, err := filepath.Abs(store.ExpandTilde(dest))
		if err != nil {
			return "", fmt.Errorf("absolutise %q: %w", dest, err)
		}
		a = store.Alias{Path: filepath.ToSlash(abs)}
		s.Set(name, a)
		if err := store.SaveStore(home, s); err != nil {
			return "", err
		}
		// Side effect: print registration message to stderr.
		fmt.Fprintf(os.Stderr, "registered %s -> %s\n", strings.ToLower(name), abs)
		return abs, nil
	}

	return filepath.FromSlash(a.Path), nil
}

func resolveSegmented(home, input string) (string, error) {
	segs, alias := segments.ParseSegmentedAlias(input)
	if len(segs) == 0 || alias == "" {
		return "", fmt.Errorf("invalid segmented alias %q (usage: <seg>@[<seg>@...]<alias>)", input)
	}

	s, err := store.LoadStore(home)
	if err != nil {
		return "", err
	}
	a, ok := s.Lookup(alias)
	if !ok {
		return "", fmt.Errorf("unknown alias %q", alias)
	}

	segFile, err := segments.LoadSegments(home)
	if err != nil {
		return "", err
	}

	target := a.Path
	for i := len(segs) - 1; i >= 0; i-- {
		part := segments.ResolveSegment(segs[i], a.Subdirs, segFile.Subdirs)
		target = strings.TrimRight(target, "/") + "/" + strings.Trim(part, "/")
	}

	return filepath.FromSlash(target), nil
}

func ScanForAlias(data []byte, target string) (string, bool) {
	lines := bytes.Split(data, []byte{'\n'})
	for i := 0; i < len(lines); i++ {
		line := trimLine(lines[i])
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if line[0] != '[' {
			continue
		}
		end := bytes.IndexByte(line, ']')
		if end < 0 {
			continue
		}
		header := line[1:end]
		if !equalFoldASCII(header, target) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			l := trimLine(lines[j])
			if len(l) == 0 || l[0] == '#' {
				continue
			}
			if l[0] == '[' {
				return "", false
			}
			if v, ok := parsePathLine(l); ok {
				return v, true
			}
		}
		return "", false
	}
	return "", false
}

func parsePathLine(line []byte) (string, bool) {
	const prefix = "path"
	if len(line) < len(prefix)+3 {
		return "", false
	}
	if !equalFoldASCII(line[:len(prefix)], prefix) {
		return "", false
	}
	i := len(prefix)
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || line[i] != '=' {
		return "", false
	}
	i++
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return "", false
	}
	quote := line[i]
	if quote != '"' && quote != '\'' {
		return "", false
	}
	i++
	start := i

	if quote == '\'' {
		end := bytes.IndexByte(line[start:], '\'')
		if end < 0 {
			return "", false
		}
		return string(line[start : start+end]), true
	}

	var b strings.Builder
	b.Grow(len(line) - start)
	for i < len(line) {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			next := line[i+1]
			switch next {
			case '"', '\\':
				b.WriteByte(next)
				i += 2
				continue
			case '/':
				b.WriteByte('/')
				i += 2
				continue
			}
			return "", false
		}
		if c == '"' {
			return b.String(), true
		}
		b.WriteByte(c)
		i++
	}
	return "", false
}

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

func equalFoldASCII(a []byte, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
