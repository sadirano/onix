package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/segments"
)

// withStdin replaces os.Stdin with a pipe, writes input to it (and closes), and
// runs fn synchronously. The original os.Stdin is restored on return.
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	orig := os.Stdin
	t.Cleanup(func() { os.Stdin = orig })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r

	done := make(chan struct{})
	go func() {
		_, _ = io.WriteString(w, input)
		_ = w.Close()
		close(done)
	}()

	fn()
	<-done
}

func TestReadLine(t *testing.T) {
	t.Run("returns trimmed line", func(t *testing.T) {
		var got string
		var ok bool
		withStdin(t, "hello world\n", func() {
			_, stderr, _ := captureStdio(func() error {
				got, ok = readLine("prompt> ", os.Stderr, os.Stdin)
				return nil
			})
			if !strings.Contains(stderr, "prompt>") {
				t.Errorf("prompt not written to stderr: %q", stderr)
			}
		})
		if !ok {
			t.Fatal("readLine reported failure on valid input")
		}
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("returns false on closed stdin", func(t *testing.T) {
		var ok bool
		withStdin(t, "", func() {
			_, _, _ = captureStdio(func() error {
				_, ok = readLine("> ", os.Stderr, os.Stdin)
				return nil
			})
		})
		if ok {
			t.Error("readLine should report failure when stdin yields no line")
		}
	})
}

func TestPromptDestination(t *testing.T) {
	t.Run("returns user input", func(t *testing.T) {
		var got string
		withStdin(t, "/some/path\n", func() {
			_, _, _ = captureStdio(func() error {
				got = promptDestination("foo", os.Stderr, os.Stdin)
				return nil
			})
		})
		if got != "/some/path" {
			t.Errorf("got %q, want %q", got, "/some/path")
		}
	})

	t.Run("returns empty on cancel", func(t *testing.T) {
		var got string
		withStdin(t, "", func() {
			_, _, _ = captureStdio(func() error {
				got = promptDestination("foo", os.Stderr, os.Stdin)
				return nil
			})
		})
		if got != "" {
			t.Errorf("got %q, want empty on cancel", got)
		}
	})
}

func TestPromptSelection(t *testing.T) {
	// Hide fzf so the numeric fallback is exercised.
	t.Setenv("PATH", t.TempDir())

	t.Run("empty options returns empty", func(t *testing.T) {
		if got := promptSelection(nil, os.Stderr, os.Stdin); got != "" {
			t.Errorf("got %q, want empty for nil options", got)
		}
	})

	t.Run("numeric selection picks option", func(t *testing.T) {
		var got string
		withStdin(t, "2\n", func() {
			_, _, _ = captureStdio(func() error {
				got = promptSelection([]string{"first", "second", "third"}, os.Stderr, os.Stdin)
				return nil
			})
		})
		if got != "second" {
			t.Errorf("got %q, want second", got)
		}
	})

	t.Run("blank input cancels", func(t *testing.T) {
		var got string
		withStdin(t, "\n", func() {
			_, _, _ = captureStdio(func() error {
				got = promptSelection([]string{"a", "b"}, os.Stderr, os.Stdin)
				return nil
			})
		})
		if got != "" {
			t.Errorf("got %q, want empty on blank input", got)
		}
	})

	t.Run("out-of-range index cancels", func(t *testing.T) {
		var got string
		withStdin(t, "99\n", func() {
			_, _, _ = captureStdio(func() error {
				got = promptSelection([]string{"a", "b"}, os.Stderr, os.Stdin)
				return nil
			})
		})
		if got != "" {
			t.Errorf("got %q, want empty on out-of-range index", got)
		}
	})

	t.Run("non-numeric input cancels", func(t *testing.T) {
		var got string
		withStdin(t, "notanumber\n", func() {
			_, _, _ = captureStdio(func() error {
				got = promptSelection([]string{"a", "b"}, os.Stderr, os.Stdin)
				return nil
			})
		})
		if got != "" {
			t.Errorf("got %q, want empty on non-numeric input", got)
		}
	})
}

func TestPromptSegmentDefinition(t *testing.T) {
	// Hide fzf so the numeric fallback is exercised. The fzf path needs a TTY
	// to be useful and would consume stdin out from under the test's pipe.
	t.Setenv("PATH", t.TempDir())

	t.Run("template source persists", func(t *testing.T) {
		home := t.TempDir()
		var cd *segments.ContextDef
		var perr error
		// Inputs: choose template, then template body, then accept save.
		withStdin(t, "1\n/tickets/${tasks}\ny\n", func() {
			_, _, _ = captureStdio(func() error {
				cd, perr = promptSegmentDefinition(home, "tasks", "42", os.Stderr, os.Stdin)
				return nil
			})
		})
		if perr != nil {
			t.Fatalf("prompt error: %v", perr)
		}
		if cd == nil {
			t.Fatal("prompt returned nil context")
		}
		if cd.SourceTemplate != "/tickets/${tasks}" {
			t.Errorf("template = %q, want /tickets/${tasks}", cd.SourceTemplate)
		}

		// File must exist and contain the new context.
		sf, err := segments.LoadSegments(home)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		got, ok := segments.LookupContext(sf, "tasks")
		if !ok {
			t.Fatal("tasks context not persisted")
		}
		if got.SourceTemplate != "/tickets/${tasks}" {
			t.Errorf("persisted template = %q", got.SourceTemplate)
		}
	})

	t.Run("decline save aborts", func(t *testing.T) {
		home := t.TempDir()
		var cd *segments.ContextDef
		// Template chosen, but the [Y/n] gets "n" → cancel.
		withStdin(t, "1\n/${x}\nn\n", func() {
			_, _, _ = captureStdio(func() error {
				cd, _ = promptSegmentDefinition(home, "x", "", os.Stderr, os.Stdin)
				return nil
			})
		})
		if cd != nil {
			t.Errorf("got context %+v, want nil on declined save", cd)
		}
		// segments.toml must not have been created.
		if _, err := os.Stat(segments.Path(home)); !os.IsNotExist(err) {
			t.Errorf("segments.toml created despite cancel: %v", err)
		}
	})

	t.Run("blank kind cancels", func(t *testing.T) {
		home := t.TempDir()
		var cd *segments.ContextDef
		withStdin(t, "\n", func() {
			_, _, _ = captureStdio(func() error {
				cd, _ = promptSegmentDefinition(home, "x", "", os.Stderr, os.Stdin)
				return nil
			})
		})
		if cd != nil {
			t.Errorf("got context %+v, want nil on blank choice", cd)
		}
	})

	t.Run("unrecognised kind errors", func(t *testing.T) {
		home := t.TempDir()
		var perr error
		withStdin(t, "9\n", func() {
			_, _, _ = captureStdio(func() error {
				_, perr = promptSegmentDefinition(home, "x", "", os.Stderr, os.Stdin)
				return nil
			})
		})
		if perr == nil {
			t.Error("expected error for unknown choice")
		}
	})
}
