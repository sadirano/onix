package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
