package segments

import (
	"fmt"
	"strings"
)

// ExpandTemplate walks tmpl and replaces every ${name} reference with the
// value returned by lookup(name). A `$` not immediately followed by `{` is
// a literal. An unterminated `${...` is an error.
//
// Resolution semantics:
//   - lookup returns (value, true) → substitute value (empty string is allowed).
//   - lookup returns (_, false)    → unresolved-variable error, mentioning name.
//
// The where argument is included in the error message so callers can point
// users at the offending field ("source-template", "source-exec[2]", etc.).
func ExpandTemplate(tmpl, where string, lookup func(name string) (string, bool)) (string, error) {
	if !strings.Contains(tmpl, "${") {
		return tmpl, nil
	}
	var b strings.Builder
	b.Grow(len(tmpl))
	i := 0
	for i < len(tmpl) {
		c := tmpl[i]
		if c != '$' || i+1 >= len(tmpl) || tmpl[i+1] != '{' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(tmpl[i+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("%s: unterminated ${...} starting at offset %d", where, i)
		}
		name := tmpl[i+2 : i+2+end]
		if name == "" {
			return "", fmt.Errorf("%s: empty variable name in ${} at offset %d", where, i)
		}
		v, ok := lookup(name)
		if !ok {
			return "", fmt.Errorf("%s: unresolved variable ${%s}", where, name)
		}
		b.WriteString(v)
		i += 2 + end + 1
	}
	return b.String(), nil
}

// GuardFragment rejects a post-expansion fragment that would let a template
// escape its alias.
//
// Rules:
//   - No null byte anywhere.
//   - Splitting on `/` and `\`, no component may equal `..`.
//   - The fragment may begin with at most one leading `/` (the template's
//     directory-separator prefix). A second `/`, a leading `\`, a leading
//     `~`, or a leading drive-letter pattern (e.g. `C:`) is rejected.
//
// The segment argument is included in the error message so callers don't
// have to wrap.
func GuardFragment(segment, fragment string) error {
	if strings.IndexByte(fragment, 0) >= 0 {
		return fmt.Errorf("segment %q: fragment contains null byte", segment)
	}

	// Leading-prefix check.
	rest := fragment
	if strings.HasPrefix(rest, "/") {
		// Single leading `/` is OK; strip it for the per-component scan
		// and ensure the next byte is not also a separator.
		rest = rest[1:]
		if strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "\\") {
			return fmt.Errorf("segment %q: fragment %q escaped its alias", segment, fragment)
		}
	}
	if strings.HasPrefix(rest, "\\") || strings.HasPrefix(rest, "~") {
		return fmt.Errorf("segment %q: fragment %q escaped its alias", segment, fragment)
	}
	if hasDriveLetterPrefix(rest) {
		return fmt.Errorf("segment %q: fragment %q escaped its alias", segment, fragment)
	}

	// Component scan on the original fragment so `../foo` is caught.
	for _, part := range splitPathSeps(fragment) {
		if part == ".." {
			return fmt.Errorf("segment %q: fragment %q escaped its alias", segment, fragment)
		}
	}
	return nil
}

func hasDriveLetterPrefix(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return false
	}
	return s[1] == ':'
}

func splitPathSeps(s string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
