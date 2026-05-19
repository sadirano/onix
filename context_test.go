package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/segments"
)

func TestContextListCmd(t *testing.T) {
	home := t.TempDir()
	segmentsPath := filepath.Join(home, "segments.toml")
	content := `
[[contexts]]
segment = "src"
source-template = "/source"
env     = { GOFLAGS = "-tags=integration" }
`
	if err := os.WriteFile(segmentsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureStdio(func() error {
		return (&ContextListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"SEGMENT", "src", "GOFLAGS", "template=/source"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q: %q", want, stdout)
		}
	}
}

func TestContextListCmd_EmptySegments(t *testing.T) {
	home := t.TempDir()
	stdout, _, err := captureStdio(func() error {
		return (&ContextListCmd{}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "no contexts defined") {
		t.Errorf("expected empty-state message, got: %q", stdout)
	}
}

func TestSourceSummary(t *testing.T) {
	cases := []struct {
		name string
		cd   segments.ContextDef
		want string
	}{
		{"template", segments.ContextDef{SourceTemplate: "/docs"}, "template=/docs"},
		{"exec", segments.ContextDef{SourceExec: []string{"git", "rev-parse", "HEAD"}}, "exec=git rev-parse HEAD"},
		{"file", segments.ContextDef{SourceFile: "@home/state/x"}, "file=@home/state/x"},
		{"none", segments.ContextDef{}, "-"},
	}
	for _, tc := range cases {
		if got := sourceSummary(tc.cd); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
