package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fastListNames prints alias names from aliases.toml, one per line, in
// sorted order. Used by the PowerShell tab-completer ($onixAliasCompleter)
// which fires every Tab press — so we keep this code path equally lean to
// fastResolve. We don't enforce TOML correctness here; if the file is
// malformed the user will hit a real error on the next `onix resolve` and
// we'd rather not spam completion output with parse failures.
func fastListNames(home string) error {
	data, err := os.ReadFile(aliasesPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty list is a valid completion result
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
	// Names are already lowercase on disk (Store normalises on Save), but
	// hand-edited files might not be. Sort for stable output regardless.
	sort.Strings(names)
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

// fastResolve is the hot-path implementation of `onix resolve <name>`.
//
// It bypasses kong, go-toml, and the Store struct entirely. The schema of
// aliases.toml is fixed and trivial:
//
//	[alias_name]
//	path = "C:/some/path"
//
// so we can scan for the target section header and pull the `path = "..."`
// value with a few byte-level operations. Even on a 200-alias file this
// is sub-millisecond. The slow path (Load/Save/Set/etc.) keeps using
// go-toml so we don't have to reimplement a real TOML parser for the
// management commands.
//
// When the input contains '@' we route to the slow path: segment
// resolution needs the per-alias subdir map and the global segments
// registry, both of which are easier to read through go-toml than via
// the byte scanner. The slow path still bypasses kong (no reflection).
//
// Correctness fence: if the file uses anything unusual (multiline strings,
// inline tables, escape sequences other than \", \\ , \/) we fall back to
// the full TOML parser via the Store API. That way exotic edits don't
// silently misbehave — they just take the regular path.
func fastResolve(home, name string) error {
	// Segmented input takes a different shape (per-alias subdirs +
	// global registry) so we don't even try the byte scanner on it.
	if strings.Contains(name, "@") {
		return resolveSegmented(home, name)
	}

	data, err := os.ReadFile(aliasesPath(home))
	if err != nil {
		// Match the error shape from the slow path so the user sees the
		// same message regardless of which code path they hit.
		if os.IsNotExist(err) {
			return fmt.Errorf("unknown alias %q (run: onix list)", name)
		}
		return err
	}

	target := strings.ToLower(strings.TrimSpace(name))
	if p, ok := scanForAlias(data, target); ok {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", p, err)
		}
		// Print exactly one line — same contract as the kong path so the
		// PowerShell `o` wrapper doesn't need to care which we took.
		fmt.Println(p)
		return nil
	}

	// Either the alias isn't present or the file uses a TOML construct
	// the fast scanner doesn't handle. Fall back to the full loader so
	// hand-edited exotic syntax still works.
	s, err := LoadStore(home)
	if err != nil {
		return err
	}
	a, ok := s.Lookup(name)
	if !ok {
		if err := ValidateAliasName(name); err != nil {
			return err
		}
		dest := promptDestination(name)
		if dest == "" {
			return fmt.Errorf("unknown alias %q (run: onix list)", name)
		}
		abs, err := filepath.Abs(expandTilde(dest))
		if err != nil {
			return fmt.Errorf("absolutise %q: %w", dest, err)
		}
		a = Alias{Path: filepath.ToSlash(abs)}
		s.Set(name, a)
		if err := SaveStore(home, s); err != nil {
			return err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", abs, err)
		}
		fmt.Fprintf(os.Stderr, "registered %s -> %s\n", strings.ToLower(name), abs)
		fmt.Println(abs)
		return nil
	}
	if err := os.MkdirAll(a.Path, 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", a.Path, err)
	}
	fmt.Println(a.Path)
	return nil
}

// resolveSegmented handles input shapes like `seg1@seg2@alias`.
//
// The walk is innermost-first (right-to-left) because the user's
// left-to-right syntax reads outermost-first. So `task@client@place`
// builds `<place>/<client>/<task>`. Per-alias subdirs win over the
// global registry, and an unresolved segment falls back to its literal
// name — matching v1 behaviour so unregistered segments still navigate
// reasonably.
//
// We load the full Store and the SegmentsFile via go-toml here. Per-call
// cost is roughly 1–2ms on top of the OS process-spawn floor, which is
// fine: segmented input is for richer navigation, not the hot hot path.
func resolveSegmented(home, input string) error {
	target, err := resolveSegmentedToPath(home, input)
	if err != nil {
		return err
	}
	fmt.Println(target)
	return nil
}

// resolveSegmentedToPath returns the resolved path without printing it.
// Used by resolveAliasPath (which feeds the path to chdir/exec) so the
// segment-walk logic doesn't live in two places.
func resolveSegmentedToPath(home, input string) (string, error) {
	segments, alias := ParseSegmentedAlias(input)
	if len(segments) == 0 || alias == "" {
		// Empty alias or no segments parsed — give a clear error rather
		// than silently treating malformed input as a regular alias.
		return "", fmt.Errorf("invalid segmented alias %q (usage: <seg>@[<seg>@...]<alias>)", input)
	}

	s, err := LoadStore(home)
	if err != nil {
		return "", err
	}
	a, ok := s.Lookup(alias)
	if !ok {
		return "", fmt.Errorf("unknown alias %q (run: onix list)", alias)
	}

	segFile, err := LoadSegments(home)
	if err != nil {
		return "", err
	}

	// Start from the alias's path (already in forward-slash form on
	// disk; we keep it that way so we can hand the result to PowerShell's
	// Set-Location without conversion).
	target := a.Path
	for i := len(segments) - 1; i >= 0; i-- {
		part := ResolveSegment(segments[i], a.Subdirs, segFile.Subdirs)
		// Use forward-slash joins so the printed output stays the same
		// shape regardless of platform. filepath.FromSlash on the
		// consumer side (or PowerShell's native handling) sorts out the
		// separator if needed.
		target = strings.TrimRight(target, "/") + "/" + strings.Trim(part, "/")
	}

	target = filepath.FromSlash(target)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create directory %q: %w", target, err)
	}
	return target, nil
}

// scanForAlias walks the file byte by byte looking for the section header
// `[target]` (case-insensitive, no spaces, no quoted-key syntax) followed
// by a `path = "..."` line. Returns the unquoted path string and true on
// match, or "" and false if the schema didn't match exactly.
//
// We accept either CRLF or LF line endings, ignore comment lines, and
// stop scanning once we hit the next [section] header (so a malformed
// section without a path doesn't bleed into the next one).
func scanForAlias(data []byte, target string) (string, bool) {
	lines := bytes.Split(data, []byte{'\n'})
	for i := 0; i < len(lines); i++ {
		line := trimLine(lines[i])
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		// Section header?
		if line[0] != '[' {
			continue
		}
		end := bytes.IndexByte(line, ']')
		if end < 0 {
			continue
		}
		// Lowercase comparison without allocating — most aliases are
		// already lowercase on disk so this almost always matches the
		// first byte and returns fast.
		header := line[1:end]
		if !equalFoldASCII(header, target) {
			continue
		}
		// Look ahead for `path = "..."`, skipping blank/comment lines,
		// bailing on a new section so we don't read a sibling's path.
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
			// Some other key (we don't recognise it yet). Keep scanning
			// in case `path` comes later in the same section.
		}
		return "", false
	}
	return "", false
}

// parsePathLine returns the unquoted value of a `path = "..."` line, or
// (""", false) if the line isn't a recognised path declaration. We only
// handle the basic-string form (double-quoted, no escapes beyond \\ and \");
// anything else (literal strings, multi-line, escapes) sends us back to
// go-toml via the fallback.
func parsePathLine(line []byte) (string, bool) {
	const prefix = "path"
	if len(line) < len(prefix)+3 {
		return "", false
	}
	if !equalFoldASCII(line[:len(prefix)], prefix) {
		return "", false
	}
	// Skip whitespace and the '=' between key and value.
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
	if i >= len(line) || line[i] != '"' {
		return "", false
	}
	// Find the closing quote, skipping \" escapes.
	i++
	start := i
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
			// Unknown escape — bail to fall back path.
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

// trimLine trims a single line of leading/trailing spaces, tabs, and the
// trailing CR if the file uses CRLF endings. We don't use strings.TrimSpace
// because it allocates a string conversion per call.
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

// equalFoldASCII is a tight ASCII case-insensitive comparison between a
// byte slice and a string. We avoid bytes.EqualFold because it handles
// Unicode folding (a few percent of cycles we don't need); alias names
// and TOML keys are ASCII in practice.
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
