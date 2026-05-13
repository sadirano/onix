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

// ParseSegmentedAlias splits "seg1@seg2@...@alias" into the segments list and the alias name.
func ParseSegmentedAlias(input string) (segments []string, alias string) {
	i := strings.LastIndex(input, "@")
	if i < 0 {
		return nil, input
	}
	left := input[:i]
	alias = input[i+1:]
	for _, s := range strings.Split(left, "@") {
		if strings.TrimSpace(s) != "" {
			segments = append(segments, s)
		}
	}
	return segments, alias
}
