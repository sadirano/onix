package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ContextDef is one [[contexts]] entry in segments.toml. It declares
// what the shell should do after cd-ing into a segmented alias whose
// leading segment matches Segment. Env vars are set in the calling shell
// (via Invoke-Expression of the printed PowerShell statements); Exec, if
// present, is run after the env vars are applied.
//
// First-defined wins when multiple entries share the same Segment name.
type ContextDef struct {
	Segment string            `toml:"segment"`
	Env     map[string]string `toml:"env,omitempty"`
	Exec    []string          `toml:"exec,omitempty"`
}

// SegmentsFile is the on-disk shape of ~/.onix/segments.toml. [subdirs]
// maps segment names to path fragments; [[contexts]] declares what the
// shell does after landing in a segmented directory. Both live in the
// same file so users have one place for everything @-segment related.
//
// Layout:
//
//	[subdirs]
//	docs = "documentation"
//	src  = "source"
//
//	[[contexts]]
//	segment = "src"
//	env     = { GO111MODULE = "on" }
//	exec    = ["make", "dev-env"]
//
// Per-alias path overrides live on the Alias struct itself (aliases.toml)
// so the override is visually attached to the alias being overridden.
type SegmentsFile struct {
	Subdirs  map[string]string `toml:"subdirs,omitempty"`
	Contexts []ContextDef      `toml:"contexts,omitempty"`
}

// LoadSegments reads ~/.onix/segments.toml. A missing file is fine — it
// just means the global registry is empty and every `<seg>@<alias>`
// lookup either hits a per-alias override or falls through to the
// literal segment name. Bad TOML is reported with the path so the user
// can find the file to fix.
func LoadSegments(home string) (*SegmentsFile, error) {
	p := segmentsConfigPath(home)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &SegmentsFile{Subdirs: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	sf := &SegmentsFile{}
	if err := toml.Unmarshal(data, sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if sf.Subdirs == nil {
		sf.Subdirs = map[string]string{}
	}
	return sf, nil
}

// segmentsConfigPath returns ~/.onix/segments.toml.
func segmentsConfigPath(home string) string {
	return filepath.Join(home, "segments.toml")
}

// ResolveSegment maps one segment name to a path fragment using the
// override chain: per-alias subdirs > global subdirs > literal name.
// Both maps are looked up case-insensitively so users can type the
// segment in any casing without thinking about it.
//
// The literal-fallback is intentional: it lets users type
// `o anything@acme` and land in <acme>/anything without any prior setup.
// That matches v1's behaviour and keeps the syntax discoverable.
func ResolveSegment(seg string, aliasSubs, globalSubs map[string]string) string {
	if v, ok := lookupCaseInsensitive(aliasSubs, seg); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := lookupCaseInsensitive(globalSubs, seg); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return seg
}

// lookupCaseInsensitive is a tiny helper because go-toml decodes keys
// verbatim and users may have mixed casing in their own files. We pay
// the linear-scan cost per lookup, but the maps are tiny (single-digit
// to a few dozen entries) so it's never the hot-path bottleneck.
func lookupCaseInsensitive(m map[string]string, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	if v, ok := m[key]; ok {
		return v, true // exact match first to skip the loop in the common case
	}
	target := strings.ToLower(key)
	for k, v := range m {
		if strings.ToLower(k) == target {
			return v, true
		}
	}
	return "", false
}

// ParseSegmentedAlias splits "seg1@seg2@...@alias" into the segments
// list (in user/left-to-right order) and the alias name. Returns nil
// segments when the input has no '@' — that's the simple-alias case
// callers can hand back to the existing resolver.
//
// We use LastIndex of '@' so an alias name itself containing '@' (rare
// but legal in our schema) doesn't get mangled. A leading or duplicated
// '@' yields empty segments which we filter out — same friendliness as
// v1's parser.
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
