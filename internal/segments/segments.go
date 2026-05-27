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
// Env is consulted during resolve-time variable lookup inside templates
// and source-exec args; it is not exported to the shell after cd.
//
// Scope controls visibility when the entry lives in the global
// ~/.onix/segments.toml file. Set scope = "global" to make the entry
// available to all aliases (the old implicit behaviour). Without it, the
// entry is ignored during global lookup — it must be in a per-alias file
// (~/.onix/segments/<alias>.toml or <alias>/.onix/segments.toml) to
// take effect.
type ContextDef struct {
	Segment        string            `toml:"segment"`
	Scope          string            `toml:"scope,omitempty"`
	Param          string            `toml:"param,omitempty"`
	SourceTemplate string            `toml:"source-template,omitempty"`
	SourceExec     []string          `toml:"source-exec,omitempty"`
	SourceFile     string            `toml:"source-file,omitempty"`
	Env            map[string]string `toml:"env,omitempty"`
}

// SegmentsFile is the on-disk shape of ~/.onix/segments.toml.
type SegmentsFile struct {
	Contexts []ContextDef `toml:"contexts,omitempty"`
}

// Path returns home/segments.toml.
func Path(home string) string {
	return filepath.Join(home, "segments.toml")
}

// LookupContext finds the [[contexts]] entry whose Segment matches name
// case-insensitively. When multiple entries share a name, the first one
// wins (so the TOML author controls precedence by ordering).
// Use this for per-alias files (local and central) where every entry is
// implicitly scoped to the alias.
func LookupContext(sf *SegmentsFile, name string) (*ContextDef, bool) {
	if sf == nil {
		return nil, false
	}
	target := strings.ToLower(name)
	for i := range sf.Contexts {
		if strings.ToLower(sf.Contexts[i].Segment) == target {
			return &sf.Contexts[i], true
		}
	}
	return nil, false
}

// LookupGlobalContext finds a [[contexts]] entry in the global segments.toml
// that has scope = "global". Entries without an explicit scope are not
// returned — they must live in a per-alias file to take effect.
func LookupGlobalContext(sf *SegmentsFile, name string) (*ContextDef, bool) {
	if sf == nil {
		return nil, false
	}
	target := strings.ToLower(name)
	for i := range sf.Contexts {
		cd := &sf.Contexts[i]
		if strings.ToLower(cd.Segment) == target && strings.ToLower(cd.Scope) == "global" {
			return cd, true
		}
	}
	return nil, false
}

// SaveSegments writes sf to home/segments.toml atomically.
//
// The file is round-tripped through go-toml/v2's marshaller; hand-written
// comments in the original are not preserved.
func SaveSegments(home string, sf *SegmentsFile) error {
	return SaveSegmentsFile(Path(home), sf)
}

// LocalPath returns the per-alias segments.toml path for aliasBase.
func LocalPath(aliasBase string) string {
	return filepath.Join(aliasBase, ".onix", "segments.toml")
}

// CentralPath returns the central per-alias segments file path under the
// onix home. Files live in ~/.onix/segments/<alias>.toml and are named by
// the lowercase alias to avoid filesystem case quirks.
func CentralPath(home, alias string) string {
	return filepath.Join(home, "segments", strings.ToLower(alias)+".toml")
}

// SaveSegmentsFile writes sf to the exact file path atomically. Parent
// directories are created as needed.
func SaveSegmentsFile(filePath string, sf *SegmentsFile) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(filePath), err)
	}
	data, err := toml.Marshal(sf)
	if err != nil {
		return fmt.Errorf("marshal segments: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".segments.*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// LoadSegmentsFile reads segments from the exact file path. Missing files
// yield an empty SegmentsFile without error.
func LoadSegmentsFile(filePath string) (*SegmentsFile, error) {
	data, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return &SegmentsFile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}
	sf := &SegmentsFile{}
	if err := toml.Unmarshal(data, sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}

	for i := range sf.Contexts {
		cd := &sf.Contexts[i]
		if err := store.ValidateSegmentName(cd.Segment); err != nil {
			return nil, fmt.Errorf("%s: context: %w", filePath, err)
		}
		if err := validateSources(cd); err != nil {
			return nil, fmt.Errorf("%s: context %q: %w", filePath, cd.Segment, err)
		}
	}

	return sf, nil
}

// LoadSegments reads ~/.onix/segments.toml.
func LoadSegments(home string) (*SegmentsFile, error) {
	return LoadSegmentsFile(Path(home))
}

// validateSources rejects a context that sets more than one source-* field.
// Zero source-* fields is allowed: env-/exec-only contexts contribute no
// path fragment.
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
// An empty inline value (`seg:`) is treated as "no inline value": HasValue
// is false and Value is "".
type ParsedSegment struct {
	Name     string
	Value    string
	HasValue bool
}

// ParseSegmentedAlias splits "seg1[:v1]@seg2[:v2]@...@alias" into the
// segments list and the alias name. Empty segments (from consecutive `@`s)
// are dropped.
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
