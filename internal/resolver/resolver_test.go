package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/store"
)

// TestScanForAlias_AgreesWithStore is the canonical contract test for the
// fast path: it must return exactly what LoadStore + Lookup would return
// for the same input.
func TestScanForAlias_AgreesWithStore(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	cases := map[string]string{
		"acme":  "C:/projects/acme",
		"sms":   "D:/work/sms",
		"funky": "/var/lib/some path",
	}
	for k, v := range cases {
		s.Set(k, store.Alias{Path: v})
	}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(store.AliasesPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for name, want := range cases {
		got, ok := ScanForAlias(data, name)
		if !ok {
			t.Errorf("scan(%q): not found", name)
			continue
		}
		if got != want {
			t.Errorf("scan(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestScanForAlias_MissingFallsThrough confirms the scanner returns
// (false) for an absent alias.
func TestScanForAlias_MissingFallsThrough(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"a": {Path: "C:/a"}}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.AliasesPath(dir))
	if _, ok := ScanForAlias(data, "missing"); ok {
		t.Error("ScanForAlias claimed to find an absent alias")
	}
}

// TestScanForAlias_HandlesQuotes makes sure paths containing escaped
// quotes round-trip correctly.
func TestScanForAlias_HandlesQuotes(t *testing.T) {
	const path = `C:/weird "name"/proj`
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"q": {Path: path}}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.AliasesPath(dir))
	got, ok := ScanForAlias(data, "q")
	if !ok || got != path {
		t.Errorf("scan(q) = %q,%v want %q,true", got, ok, path)
	}
}

// TestScanForAlias_StopsAtNextSection makes sure a section without a
// `path` doesn't read into the next section's value.
func TestScanForAlias_StopsAtNextSection(t *testing.T) {
	data := []byte(strings.Join([]string{
		`[a]`,
		``,
		`[b]`,
		`path = "C:/b"`,
		``,
	}, "\n"))
	if _, ok := ScanForAlias(data, "a"); ok {
		t.Error("ScanForAlias bled into sibling section")
	}
}

// BenchmarkScanForAlias measures the hot-path cost of the scanner.
func BenchmarkScanForAlias(b *testing.B) {
	dir := b.TempDir()
	seedStore(b, dir, 200)
	data, err := os.ReadFile(store.AliasesPath(dir))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := ScanForAlias(data, "alias100"); !ok {
			b.Fatal("alias100 missing")
		}
	}
}

// BenchmarkResolve_Segmented_Template measures the cost of a single
// templated segment over a populated store. Establishes a baseline so
// future segments-stack changes can be benchstat'd against it.
func BenchmarkResolve_Segmented_Template(b *testing.B) {
	dir := b.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{Path: "C:/projects/acme"})
	if err := store.SaveStore(dir, s); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(`[[contexts]]
segment = "docs"
source-template = "/documentation"
`), 0o644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Resolve(dir, "docs@acme", nil, nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func seedStore(t testing.TB, dir string, count int) {
	s := &store.Store{Aliases: map[string]store.Alias{}}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("alias%d", i)
		s.Set(name, store.Alias{Path: "C:/path/to/" + name})
	}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
}

// TestResolve_Segmented captures the [[contexts]]-driven resolution rules.
// The path joiner is now verbatim: templates own their separators.
func TestResolve_Segmented(t *testing.T) {
	dir := t.TempDir()

	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{Path: "C:/projects/acme"})
	s.Set("vanilla", store.Alias{Path: "C:/projects/vanilla/"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(`[[contexts]]
segment = "docs"
source-template = "/documentation"

[[contexts]]
segment = "src"
source-template = "/source"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in   string
		want string
	}{
		{"docs@acme", "C:/projects/acme/documentation"},
		// Trailing `/` on the alias path is stripped before appending.
		{"docs@vanilla", "C:/projects/vanilla/documentation"},
		{"src@docs@vanilla", "C:/projects/vanilla/documentation/source"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Resolve(dir, tc.in, nil, nil, nil)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			want := filepath.FromSlash(tc.want)
			if got != want {
				t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, want)
			}
		})
	}
}

// TestResolve_Segmented_InlineValue locks the `seg:value` flow: the inline
// value binds to ${<param>} (default: <segment>) inside the template.
func TestResolve_Segmented_InlineValue(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("proja", store.Alias{Path: "C:/proja"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(`[[contexts]]
segment = "tasks"
source-template = "/${tasks}"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(dir, "tasks:123@proja", nil, nil, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.FromSlash("C:/proja/123")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolve_Segmented_NoLeadingSlash exercises the "templates own their
// separators" rule: a template without a leading `/` appends directly, so
// two segments can compose into a single filename.
func TestResolve_Segmented_NoLeadingSlash(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("projb", store.Alias{Path: "C:/projectb/"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(`[[contexts]]
segment = "client"
source-template = "/${client}"

[[contexts]]
segment = "task"
source-template = "_${task}.md"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(dir, "task:432@client:bob@projb", nil, nil, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.FromSlash("C:/projectb/bob_432.md")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolve_Segmented_TraversalRejected confirms that a template
// expanding to a `..` component is caught by GuardFragment.
func TestResolve_Segmented_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("home", store.Alias{Path: "C:/home"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segments.toml"), []byte(`[[contexts]]
segment = "evil"
source-template = "/${target}"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("target", "../etc/passwd")
	_, err := Resolve(dir, "evil@home", nil, nil, nil)
	if err == nil {
		t.Fatal("expected traversal guard to reject ../etc/passwd")
	}
	if !strings.Contains(err.Error(), "evil") {
		t.Errorf("error should mention segment name 'evil': %v", err)
	}
}

// TestResolve_Segmented_UnknownNoPrompt confirms that an unknown segment
// with a nil prompter is a hard error (the --no-prompt path).
func TestResolve_Segmented_UnknownNoPrompt(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{Path: "C:/acme"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(dir, "mystery@acme", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for undefined segment")
	}
	if !strings.Contains(err.Error(), "mystery") {
		t.Errorf("error should mention segment name: %v", err)
	}
}

// TestResolve_Segmented_UnknownWithPrompt drives the prompt callback,
// asserts the resolver picks up the newly-defined context, and verifies
// the context was persisted to segments.toml.
func TestResolve_Segmented_UnknownWithPrompt(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{Path: "C:/acme"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}

	calls := 0
	prompter := func(segmentName, inlineValue string) (*segments.ContextDef, error) {
		calls++
		if segmentName != "tasks" {
			t.Errorf("prompt got segment=%q, want tasks", segmentName)
		}
		if inlineValue != "42" {
			t.Errorf("prompt got inline=%q, want 42", inlineValue)
		}
		cd := segments.ContextDef{Segment: "tasks", SourceTemplate: "/tickets/${tasks}"}
		sf, err := segments.LoadSegments(dir)
		if err != nil {
			return nil, err
		}
		sf.Contexts = append(sf.Contexts, cd)
		if err := segments.SaveSegments(dir, sf); err != nil {
			return nil, err
		}
		return &cd, nil
	}

	got, err := Resolve(dir, "tasks:42@acme", nil, nil, prompter)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if calls != 1 {
		t.Errorf("prompter called %d times, want 1", calls)
	}
	want := filepath.FromSlash("C:/acme/tickets/42")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Persistence check.
	sf, err := segments.LoadSegments(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cd, ok := segments.LookupContext(sf, "tasks")
	if !ok {
		t.Fatal("expected 'tasks' context after prompt, not found")
	}
	if cd.SourceTemplate != "/tickets/${tasks}" {
		t.Errorf("template = %q, want /tickets/${tasks}", cd.SourceTemplate)
	}
}

// TestResolve_Segmented_PromptCancelled returns ErrCancelled when the
// prompter signals cancellation by returning (nil, nil).
func TestResolve_Segmented_PromptCancelled(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{}}
	s.Set("acme", store.Alias{Path: "C:/acme"})
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}
	prompter := func(string, string) (*segments.ContextDef, error) { return nil, nil }
	_, err := Resolve(dir, "mystery@acme", nil, nil, prompter)
	if err != ErrCancelled {
		t.Fatalf("got %v, want ErrCancelled", err)
	}
}

func TestResolve_Basic(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{"a": {Path: "C:/a"}}}
	_ = store.SaveStore(dir, s)

	t.Run("fast path", func(t *testing.T) {
		got, err := Resolve(dir, "a", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.FromSlash("C:/a") {
			t.Errorf("got %q, want C:/a", got)
		}
	})

	t.Run("slow path", func(t *testing.T) {
		// Delete file to force slow path (or just use non-fast alias name if any)
		// Wait, slow path is also triggered if Resolve reads full store.
		// Resolve always tries fast path first.
		got, err := Resolve(dir, "A", nil, nil, nil) // Case insensitive
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.FromSlash("C:/a") {
			t.Errorf("got %q, want C:/a", got)
		}
	})
}

// TestResolve_FuzzyMatch_DistanceLimit guards against the loose-match
// regression where short typos selected far-off candidates ("sync" → "bin",
// distance 3). The new limit is shorter-length/3, capped at 3.
func TestResolve_FuzzyMatch_DistanceLimit(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{
		"bin":  {Path: "C:/bin"},
		"onix": {Path: "C:/onix"},
		"play": {Path: "C:/play"},
	}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}

	selected := ""
	selector := func(opts []string) string {
		if len(opts) > 0 {
			selected = opts[0]
		}
		return ""
	}

	// "sync" is distance 3 from "bin" but distance 4 from "onix" / "play".
	// Under the new limit (shorter/3 = 1 for shorter=3, capped to 1) NONE
	// of these are close enough to suggest. The selector should not fire.
	_, err := Resolve(dir, "sync", nil, selector, nil)
	if err == nil {
		t.Errorf("expected error for unknown alias 'sync', got nil")
	}
	if selected != "" {
		t.Errorf("selector should not have been called for sync; got candidate %q", selected)
	}

	// Sanity: a real typo (one transposition) should still trigger the selector.
	selected = ""
	_, err = Resolve(dir, "onxi", nil, selector, nil)
	if err == nil {
		t.Errorf("expected error for unknown alias 'onxi' when selector returns empty")
	}
	if selected != "onix" {
		t.Errorf("real typo 'onxi' should suggest 'onix', got %q", selected)
	}
}

// TestResolve_NilSelector_BypassesFuzzy ensures that passing a nil selector
// completely skips the did-you-mean machinery. The hot-path fastResolve
// relies on this when --no-prompt is set, otherwise the cmd-wrapper's
// `for /f` capture would auto-pick a candidate.
func TestResolve_NilSelector_BypassesFuzzy(t *testing.T) {
	dir := t.TempDir()
	s := &store.Store{Aliases: map[string]store.Alias{
		"onix": {Path: "C:/onix"},
	}}
	if err := store.SaveStore(dir, s); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(dir, "onxi", nil, nil, nil)
	if err == nil {
		t.Errorf("expected error when selector is nil and alias is unknown")
	}
	if !strings.Contains(err.Error(), "unknown alias") {
		t.Errorf("expected 'unknown alias' error, got %v", err)
	}
}

func TestResolve_WithPrompter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new-path")

	t.Run("success", func(t *testing.T) {
		prompter := func(name string) string {
			return target
		}
		got, err := Resolve(dir, "new", prompter, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != target {
			t.Errorf("got %q, want %q", got, target)
		}
		// Verify it was saved
		s, _ := store.LoadStore(dir)
		if a, ok := s.Lookup("new"); !ok || filepath.ToSlash(a.Path) != filepath.ToSlash(target) {
			t.Errorf("alias not saved correctly: %+v", a)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		prompter := func(name string) string {
			return ""
		}
		_, err := Resolve(dir, "cancelled", prompter, nil, nil)
		if err != ErrCancelled {
			t.Fatalf("expected ErrCancelled, got %v", err)
		}
	})
}
