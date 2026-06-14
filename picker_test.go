package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// TestPickDirectory_MissingTools checks the picker degrades to an actionable
// error (not a crash) when es or fzf is absent — the common case on machines
// without Everything installed.
func TestPickDirectory_MissingTools(t *testing.T) {
	t.Run("es missing", func(t *testing.T) {
		fakeLookPath(t, map[string]bool{"fzf": true})
		_, err := pickDirectory(context.Background(), &env{Home: t.TempDir(), Stdout: os.Stdout, Stderr: os.Stderr}, "proj")
		if err == nil || !strings.Contains(err.Error(), "es") || !strings.Contains(err.Error(), "proj") {
			t.Errorf("expected es-missing error naming the alias, got %v", err)
		}
	})
	t.Run("fzf missing", func(t *testing.T) {
		fakeLookPath(t, map[string]bool{"es": true})
		_, err := pickDirectory(context.Background(), &env{Home: t.TempDir(), Stdout: os.Stdout, Stderr: os.Stderr}, "proj")
		if err == nil || !strings.Contains(err.Error(), "fzf") {
			t.Errorf("expected fzf-missing error, got %v", err)
		}
	})
}

// TestFilterExcludedDirs checks the Go-side exclusion that replaced es's
// `!path:` terms: blank lines drop, exclusion fragments match case-insensitively
// as substrings, and clean paths survive.
func TestFilterExcludedDirs(t *testing.T) {
	lines := []string{
		`C:\Sadirano\repo\proj`,
		`C:\Sadirano\repo\proj\node_modules\x`,
		`C:\Program Files\Git\proj`, // spaced exclusion — the one that broke es
		`C:\code\proj\.git\hooks`,
		``,
		`   `,
		`D:\work\PROJ\Release\bin`, // case-insensitive 'Release'
		`C:\keep\this\proj`,
	}
	excludes := []string{`node_modules`, `C:\Program Files`, `\.`, `\Release\`}
	got := filterExcludedDirs(lines, excludes)
	want := []string{`C:\Sadirano\repo\proj`, `C:\keep\this\proj`}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestNavigate_UnknownAlias covers the two unknown-alias branches of the
// navigate flow: with --no-prompt it surfaces the resolve error directly;
// with prompting it falls through to the picker (which here errors because es
// is faked missing, proving the hand-off happens).
func TestNavigate_UnknownAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ONIX_HOME", home)
	fakeLookPath(t, map[string]bool{}) // no es/fzf

	runWrapper := func(args ...string) (int, string) {
		var out, errBuf bytes.Buffer
		code := run(append([]string{"o"}, args...), strings.NewReader(""), &out, &errBuf)
		return code, errBuf.String()
	}

	t.Run("no-prompt surfaces resolve error, no picker", func(t *testing.T) {
		code, errOut := runWrapper("ghost", "-q")
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(errOut, "unknown alias") {
			t.Errorf("expected unknown-alias error, got %q", errOut)
		}
		// The picker's install hint must NOT appear under --no-prompt.
		if strings.Contains(errOut, "directory picker") {
			t.Errorf("picker should be skipped under --no-prompt: %q", errOut)
		}
	})

	t.Run("prompting falls through to the picker", func(t *testing.T) {
		code, errOut := runWrapper("ghost")
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(errOut, "directory picker") {
			t.Errorf("expected picker hand-off (es-missing hint), got %q", errOut)
		}
	})
}
