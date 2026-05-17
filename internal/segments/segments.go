package segments

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/sadirano/onix/internal/store"
)

// ContextDef is one [[contexts]] entry in segments.toml.
type ContextDef struct {
	Segment string            `toml:"segment"`
	Env     map[string]string `toml:"env,omitempty"`
	Exec    []string          `toml:"exec,omitempty"`
}

// SegmentsFile is the on-disk shape of ~/.onix/segments.toml.
type SegmentsFile struct {
	Version  int               `toml:"version"`
	Subdirs  map[string]string `toml:"subdirs,omitempty"`
	Contexts []ContextDef      `toml:"contexts,omitempty"`
}

// CurrentVersion is the latest schema version for segments.toml.
const CurrentVersion = 2

// Path returns home/segments.toml.
func Path(home string) string {
	return filepath.Join(home, "segments.toml")
}

// LoadSegments reads ~/.onix/segments.toml.
func LoadSegments(home string) (*SegmentsFile, error) {
	p := Path(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &SegmentsFile{Version: CurrentVersion, Subdirs: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	sf := &SegmentsFile{}
	if err := toml.Unmarshal(data, sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}

	if sf.Version == 0 {
		sf.Version = 2
	}

	for seg := range sf.Subdirs {
		if err := store.ValidateSegmentName(seg); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}

	for _, cd := range sf.Contexts {
		if err := store.ValidateSegmentName(cd.Segment); err != nil {
			return nil, fmt.Errorf("%s: context: %w", p, err)
		}
	}

	if sf.Subdirs == nil {
		sf.Subdirs = map[string]string{}
	}
	return sf, nil
}

// ResolveSegment maps one segment name to a path fragment.
func ResolveSegment(seg string, aliasSubs, globalSubs map[string]string) string {
	if v, ok := lookupCaseInsensitive(aliasSubs, seg); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := lookupCaseInsensitive(globalSubs, seg); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return seg
}

func lookupCaseInsensitive(m map[string]string, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	if v, ok := m[key]; ok {
		return v, true
	}
	target := strings.ToLower(key)
	for k, v := range m {
		if strings.ToLower(k) == target {
			return v, true
		}
	}
	return "", false
}

// ParsedSegment is one segment token, possibly carrying an inline value
// supplied via the `seg:value` syntax.
//
// The empty inline value (`seg:`) is treated as "no inline value" per the
// segments spec: HasValue is false, Value is "".
type ParsedSegment struct {
	Name     string
	Value    string
	HasValue bool
}

// ParseSegmentedAlias splits "seg1[:v1]@seg2[:v2]@...@alias" into the
// segments list and the alias name. Empty segments (from consecutive `@`s)
// are dropped — matching the historical TrimSpace behaviour.
//
// Inline values: the first `:` in a segment separates the segment name from
// its inline value. `a:b:c` parses as name="a", value="b:c". `seg:` (empty
// value) parses as HasValue=false.
func ParseSegmentedAlias(input string) (segments []ParsedSegment, alias string) {
	i := strings.LastIndex(input, "@")
	if i < 0 {
		return nil, input
	}
	left := input[:i]
	alias = input[i+1:]
	for _, s := range strings.Split(left, "@") {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		segments = append(segments, parseSegmentToken(trimmed))
	}
	return segments, alias
}

func parseSegmentToken(tok string) ParsedSegment {
	if j := strings.IndexByte(tok, ':'); j >= 0 {
		value := tok[j+1:]
		if value == "" {
			return ParsedSegment{Name: tok[:j]}
		}
		return ParsedSegment{Name: tok[:j], Value: value, HasValue: true}
	}
	return ParsedSegment{Name: tok}
}
