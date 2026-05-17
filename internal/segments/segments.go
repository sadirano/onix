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
//
// A context with no source-* field still drives apply-context's env / exec
// scripting; it just doesn't contribute to path resolution.
type ContextDef struct {
	Segment        string            `toml:"segment"`
	Param          string            `toml:"param,omitempty"`
	SourceTemplate string            `toml:"source-template,omitempty"`
	SourceExec     []string          `toml:"source-exec,omitempty"`
	SourceFile     string            `toml:"source-file,omitempty"`
	Env            map[string]string `toml:"env,omitempty"`
	Exec           []string          `toml:"exec,omitempty"`
}

// SegmentsFile is the on-disk shape of ~/.onix/segments.toml.
//
// The top-level [subdirs] table from prior versions is silently dropped on
// load — it has no representation here. Users who relied on it see the
// unknown-segment prompt on first use of each segment under the new
// resolver (segments-spec PR 4).
type SegmentsFile struct {
	Version  int          `toml:"version"`
	Contexts []ContextDef `toml:"contexts,omitempty"`
}

// CurrentVersion is the latest schema version for segments.toml.
const CurrentVersion = 3

// Path returns home/segments.toml.
func Path(home string) string {
	return filepath.Join(home, "segments.toml")
}

// LoadSegments reads ~/.onix/segments.toml.
func LoadSegments(home string) (*SegmentsFile, error) {
	p := Path(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &SegmentsFile{Version: CurrentVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	sf := &SegmentsFile{}
	if err := toml.Unmarshal(data, sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}

	if sf.Version == 0 {
		sf.Version = CurrentVersion
	}

	for i := range sf.Contexts {
		cd := &sf.Contexts[i]
		if err := store.ValidateSegmentName(cd.Segment); err != nil {
			return nil, fmt.Errorf("%s: context: %w", p, err)
		}
		if err := validateSources(cd); err != nil {
			return nil, fmt.Errorf("%s: context %q: %w", p, cd.Segment, err)
		}
	}

	return sf, nil
}

// validateSources enforces the spec's "exactly one of source-*" rule.
// Zero is allowed: env-/exec-only contexts contribute no path fragment.
func validateSources(cd *ContextDef) error {
	n := 0
	if cd.SourceTemplate != "" {
		n++
	}
	if len(cd.SourceExec) > 0 {
		n++
	}
	if cd.SourceFile != "" {
		n++
	}
	if n > 1 {
		return errors.New("at most one of source-template, source-exec, source-file may be set")
	}
	return nil
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
