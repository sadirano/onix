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

// Timer is the interface for recording checkpoints.
type Timer interface {
	Mark(name string)
}

// ErrCancelled is returned by Resolve when the user cancels the interactive
// destination prompt for an unknown alias.
var ErrCancelled = errors.New("prompt cancelled")

// SegmentPrompter is invoked when a segmented invocation references a name
// that has no matching [[contexts]] entry in segments.toml.
//
// The callback is expected to interact with the user, persist a new
// [[contexts]] entry to disk (either global or local to the target alias),
// and return the new ContextDef. Returning (nil, nil) is treated as
// "user cancelled" and is reported as ErrCancelled. A non-nil error is
// propagated to the caller verbatim.
//
// Note: the prompter receives aliasBase so it can offer a local save
// option (e.g., <alias>/.onix/segments.toml).
type SegmentPrompter func(segmentName, inlineValue, aliasBase, aliasName string) (*segments.ContextDef, error)

// Resolve finds the absolute path for an alias.
//
// It first attempts a fast byte-scan of aliases.toml to bypass full TOML
// parsing. If the alias isn't found or contains segments (@), it falls back
// to the slow path using the Store and SegmentsFile.
//
// If the alias is still unknown:
//  1. It computes Levenshtein distances to all known aliases.
//  2. If close matches exist and a selector is provided, it prompts the user.
//  3. If no match is selected and a prompter is provided, it prompts for a new path.
//
// segmentPrompter is invoked for segmented inputs (e.g. `task:432@projb`)
// when a referenced segment has no [[contexts]] entry. Passing nil makes
// unknown segments a hard error — used by --no-prompt callers.
//
// Resolve does NOT create directories on disk. It returns a host-native path
// (using filepath.FromSlash).
func Resolve(home, name string, segmentPrompter SegmentPrompter, t Timer) (string, error) {
	if t != nil {
		t.Mark("resolve-start")
	}

	if strings.Contains(name, "@") {
		return resolveSegmented(home, name, segmentPrompter, t)
	}

	// Hot path: Check disk aliases.
	data, err := os.ReadFile(store.AliasesPath(home))
	if t != nil {
		t.Mark("alias-file-read")
	}
	if err == nil {
		target := strings.ToLower(strings.TrimSpace(name))
		if p, ok := ScanForAlias(data, target); ok {
			// Fast path only handles single-target (path = "..."). Multi-target
			// aliases (paths = [...]) fall through to the slow path below.
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
		return "", fmt.Errorf("unknown alias %q", name)
	}
	if a.Path == "" {
		return "", fmt.Errorf("alias %q has no path set", name)
	}
	return filepath.FromSlash(a.Path), nil
}

func resolveSegmented(home, input string, prompter SegmentPrompter, t Timer) (string, error) {
	segs, alias := segments.ParseSegmentedAlias(input)
	if t != nil {
		t.Mark("parse-segmented")
	}
	if len(segs) == 0 || alias == "" {
		return "", fmt.Errorf("invalid segmented alias %q (usage: <seg>@[<seg>@...]<alias>)", input)
	}

	s, err := store.LoadStore(home)
	if t != nil {
		t.Mark("load-store")
	}
	if err != nil {
		return "", err
	}
	a, ok := s.Lookup(alias)
	if !ok {
		return "", fmt.Errorf("unknown alias %q", alias)
	}

	sfGlobal, err := segments.LoadSegments(home)
	if t != nil {
		t.Mark("load-global-segments")
	}
	if err != nil {
		return "", err
	}
	// Try to load a per-alias segments file first. Missing files are fine.
	aliasSegmentsPath := segments.LocalPath(a.Path)
	sfLocal, _ := segments.LoadSegmentsFile(aliasSegmentsPath)
	// Try central store for this alias under ~/.onix/segments/<alias>.toml
	centralPath := segments.CentralPath(home, alias)
	sfCentral, _ := segments.LoadSegmentsFile(centralPath)
	if t != nil {
		t.Mark("load-local-central-segments")
	}

	target := strings.TrimRight(a.Path, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		ps := segs[i]
		// Resolution precedence: local -> central -> global (scope="global" only).
		cd, ok := segments.LookupContext(sfLocal, ps.Name)
		if !ok {
			cd, ok = segments.LookupContext(sfCentral, ps.Name)
		}
		if !ok {
			cd, ok = segments.LookupGlobalContext(sfGlobal, ps.Name)
		}
		if !ok {
			if prompter == nil {
				return "", fmt.Errorf("segment %q is not defined in segments.toml", ps.Name)
			}
			cd, err = prompter(ps.Name, ps.Value, a.Path, alias)
			if err != nil {
				return "", err
			}
			if cd == nil {
				return "", ErrCancelled
			}
			// Reload after the prompt — the prompter could have saved to local,
			// central, or global; re-read all sources.
			sfLocal, _ = segments.LoadSegmentsFile(aliasSegmentsPath)
			sfCentral, _ = segments.LoadSegmentsFile(centralPath)
			sfGlobal, _ = segments.LoadSegments(home)
			cd, ok = segments.LookupContext(sfLocal, ps.Name)
			if !ok {
				cd, ok = segments.LookupContext(sfCentral, ps.Name)
			}
			if !ok {
				cd, ok = segments.LookupGlobalContext(sfGlobal, ps.Name)
			}
			if !ok {
				return "", fmt.Errorf("segment %q: prompter saved a context but it was not loadable", ps.Name)
			}
		}

		fragment, err := evalSegment(cd, ps, a.Path, home)
		if err != nil {
			return "", err
		}
		if fragment == "" {
			// An empty fragment contributes nothing to the path. If the user
			// wants a trailing separator they can include it in the template.
			continue
		}
		if err := segments.GuardFragment(ps.Name, fragment); err != nil {
			return "", err
		}
		target += fragment
	}

	return filepath.FromSlash(target), nil
}

// evalSegment resolves one segment's fragment by dispatching on the
// context's source-* field. A context with no source-* contributes no
// path fragment.
//
// Variable resolution chain inside templates and exec args:
//  1. Segment-bound inline value under ${<param>} (default: <segment>).
//  2. Context's static env map.
//  3. Process environment (os.LookupEnv).
func evalSegment(cd *segments.ContextDef, ps segments.ParsedSegment, aliasBase, home string) (string, error) {
	param := cd.Param
	if param == "" {
		param = cd.Segment
	}
	lookup := func(name string) (string, bool) {
		if ps.HasValue && name == param {
			return ps.Value, true
		}
		if v, ok := cd.Env[name]; ok {
			return v, true
		}
		return os.LookupEnv(name)
	}

	if cd.SourceTemplate != "" {
		return segments.EvalTemplateSource(cd.SourceTemplate, lookup)
	}
	if ps.HasValue {
		// No source-* but an inline value was supplied — there's no
		// template to interpret it. The user almost certainly wanted a
		// source-* field; surface that.
		return "", fmt.Errorf("segment %q: inline value %q has no source-template to consume it", cd.Segment, ps.Value)
	}
	return "", nil
}

func ScanForAlias(data []byte, target string) (string, bool) {
	lines := bytes.Split(data, []byte{'\n'})
	for i := 0; i < len(lines); i++ {
		line := TrimLine(lines[i])
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
			l := TrimLine(lines[j])
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

// TrimLine trims a single line of leading/trailing spaces, tabs, and the
// trailing CR. Exported so the fastresolve scanner in package main can
// reuse the exact same byte-level trim.
func TrimLine(line []byte) []byte {
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
