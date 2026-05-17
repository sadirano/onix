package segments

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestParseSegmentedAlias locks the parser contract: bare-segment form,
// `seg:value` inline-value form, and the empty-inline-value (`seg:`)
// case where HasValue must be false.
func TestParseSegmentedAlias(t *testing.T) {
	tests := []struct {
		in       string
		wantSegs []ParsedSegment
		wantAls  string
	}{
		{"acme", nil, "acme"},
		{"docs@acme", []ParsedSegment{{Name: "docs"}}, "acme"},
		{"task@client@place", []ParsedSegment{{Name: "task"}, {Name: "client"}}, "place"},
		{"a@b@c@d", []ParsedSegment{{Name: "a"}, {Name: "b"}, {Name: "c"}}, "d"},
		{"@trailing", nil, "trailing"},
		{"a@@b", []ParsedSegment{{Name: "a"}}, "b"},

		// Inline values.
		{"tasks:123@proja", []ParsedSegment{{Name: "tasks", Value: "123", HasValue: true}}, "proja"},
		{"client:bob@projb", []ParsedSegment{{Name: "client", Value: "bob", HasValue: true}}, "projb"},
		{
			"task:432@client:bob@projb",
			[]ParsedSegment{
				{Name: "task", Value: "432", HasValue: true},
				{Name: "client", Value: "bob", HasValue: true},
			},
			"projb",
		},
		// Empty inline value: HasValue is false, Value is "".
		{"seg:@a", []ParsedSegment{{Name: "seg"}}, "a"},
		// First colon wins — the remainder is the value verbatim.
		{"a:b:c@d", []ParsedSegment{{Name: "a", Value: "b:c", HasValue: true}}, "d"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			segs, als := ParseSegmentedAlias(tc.in)
			if !reflect.DeepEqual(segs, tc.wantSegs) {
				t.Errorf("segments = %#v, want %#v", segs, tc.wantSegs)
			}
			if als != tc.wantAls {
				t.Errorf("alias = %q, want %q", als, tc.wantAls)
			}
		})
	}
}

// TestLoadSegments_AllSourceTypes confirms a single [[contexts]] entry per
// source kind loads cleanly.
func TestLoadSegments_AllSourceTypes(t *testing.T) {
	dir := t.TempDir()
	body := `version = 3

[[contexts]]
segment = "tasks"
source-template = "/${tasks}"

[[contexts]]
segment = "branch"
source-exec = ["git", "rev-parse", "--abbrev-ref", "HEAD"]

[[contexts]]
segment = "current"
source-file = "@home/state/current"
`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sf, err := LoadSegments(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := len(sf.Contexts), 3; got != want {
		t.Fatalf("contexts = %d, want %d", got, want)
	}
	if sf.Contexts[0].SourceTemplate != "/${tasks}" {
		t.Errorf("template source missing: %+v", sf.Contexts[0])
	}
	if got := sf.Contexts[1].SourceExec; len(got) != 4 || got[0] != "git" {
		t.Errorf("exec source missing: %+v", sf.Contexts[1])
	}
	if sf.Contexts[2].SourceFile != "@home/state/current" {
		t.Errorf("file source missing: %+v", sf.Contexts[2])
	}
}

// TestLoadSegments_MultipleSourcesError catches a context that declares
// more than one source-* field. Error mentions the segment name.
func TestLoadSegments_MultipleSourcesError(t *testing.T) {
	dir := t.TempDir()
	body := `version = 3

[[contexts]]
segment = "ambiguous"
source-template = "/${ambiguous}"
source-file = "@home/state/x"
`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSegments(dir)
	if err == nil {
		t.Fatal("expected error for multiple sources, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error should mention segment name, got: %v", err)
	}
}

// TestLoadSegments_IgnoresLegacySubdirs confirms that an unknown top-level
// table (here a `[subdirs]` block) is silently dropped on load and produces
// zero contexts.
func TestLoadSegments_IgnoresLegacySubdirs(t *testing.T) {
	dir := t.TempDir()
	body := `version = 2

[subdirs]
docs = "documentation"
src  = "source"
`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sf, err := LoadSegments(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sf.Contexts) != 0 {
		t.Errorf("contexts = %d, want 0", len(sf.Contexts))
	}
}

// TestLoadSegments_ContextWithoutSourceIsAllowed confirms a context with no
// source-* field loads cleanly. Such a context contributes no path fragment;
// its env/exec map still drives apply-context's shell-side emission.
func TestLoadSegments_ContextWithoutSourceIsAllowed(t *testing.T) {
	dir := t.TempDir()
	body := `version = 3

[[contexts]]
segment = "prod"
env = { DEPLOY_ENV = "production" }
exec = ["kubectl", "config", "use-context", "prod"]
`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sf, err := LoadSegments(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sf.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(sf.Contexts))
	}
	cd := sf.Contexts[0]
	if cd.Env["DEPLOY_ENV"] != "production" {
		t.Errorf("env not preserved: %+v", cd.Env)
	}
	if cd.SourceTemplate != "" || len(cd.SourceExec) != 0 || cd.SourceFile != "" {
		t.Errorf("no source-* expected, got %+v", cd)
	}
}

// TestSegments_LoadMissingReturnsEmpty is the first-run path.
func TestSegments_LoadMissingReturnsEmpty(t *testing.T) {
	sf, err := LoadSegments(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sf == nil || len(sf.Contexts) != 0 {
		t.Fatalf("expected empty segments, got %+v", sf)
	}
	if sf.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", sf.Version, CurrentVersion)
	}
}

func TestLoadSegments_BadTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`invalid [ TOML`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSegments(dir)
	if err == nil {
		t.Fatal("expected error for bad TOML, got nil")
	}
}

// TestLoadSegments_InvalidSegmentName guards against an `@` or whitespace
// leaking into a [[contexts]] entry.
func TestLoadSegments_InvalidSegmentName(t *testing.T) {
	dir := t.TempDir()
	body := `version = 3

[[contexts]]
segment = "bad@name"
source-template = "/x"
`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSegments(dir); err == nil {
		t.Fatal("expected error for invalid segment name, got nil")
	}
}
