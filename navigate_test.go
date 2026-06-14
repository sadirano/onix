package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/segments"
)

// TestAutoDefineSegment covers the two templates the editor-free segment
// creator emits: a literal subdirectory for a bare segment, and a
// parameterised template when an inline value is supplied.
func TestAutoDefineSegment(t *testing.T) {
	home := t.TempDir()
	var errBuf bytes.Buffer

	cd, err := autoDefineSegment(home, "free", "", &errBuf, "play")
	if err != nil {
		t.Fatalf("bare segment: %v", err)
	}
	if cd == nil || cd.SourceTemplate != "/free/" {
		t.Fatalf("bare segment template = %+v, want /free/", cd)
	}

	cd2, err := autoDefineSegment(home, "task", "432", &errBuf, "play")
	if err != nil {
		t.Fatalf("valued segment: %v", err)
	}
	if cd2 == nil || cd2.SourceTemplate != "/${task}/" {
		t.Fatalf("valued segment template = %+v, want /${task}/", cd2)
	}

	// Both persist to the central per-alias file.
	sf, err := segments.LoadSegmentsFile(segments.CentralPath(home, "play"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := segments.LookupContext(sf, "free"); !ok {
		t.Error("free segment not persisted")
	}
	if _, ok := segments.LookupContext(sf, "task"); !ok {
		t.Error("task segment not persisted")
	}
}

// TestResolveAliasPath_AutoCreatesSegment proves a navigation/action against an
// undefined segment auto-creates it (as a subdirectory) and resolves through —
// no editor, no error.
func TestResolveAliasPath_AutoCreatesSegment(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	e := &env{Home: home, Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader("")}

	if err := (&AddCmd{Alias: "proj", Path: target}).Run(context.Background(), e); err != nil {
		t.Fatalf("register proj: %v", err)
	}

	got, err := resolveAliasPath(e, "docs@proj")
	if err != nil {
		t.Fatalf("resolveAliasPath: %v", err)
	}
	want := filepath.Join(target, "docs")
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

// TestResolveAliasPath_NoPromptSkipsSegmentCreate confirms --no-prompt makes an
// undefined segment a hard error instead of silently creating files.
func TestResolveAliasPath_NoPromptSkipsSegmentCreate(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	e := &env{Home: home, NoPrompt: true, Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader("")}
	if err := (&AddCmd{Alias: "proj", Path: target}).Run(context.Background(), e); err != nil {
		t.Fatalf("register proj: %v", err)
	}
	if _, err := resolveAliasPath(e, "docs@proj"); err == nil {
		t.Error("expected error for undefined segment under --no-prompt, got nil")
	}
}
