package main

import (
	"io"
	"os"
	"strings"
	"testing"
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
				got, ok = readLine("prompt> ")
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
				_, ok = readLine("> ")
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
				got = promptDestination("foo")
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
				got = promptDestination("foo")
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
		if got := promptSelection(nil); got != "" {
			t.Errorf("got %q, want empty for nil options", got)
		}
	})

	t.Run("numeric selection picks option", func(t *testing.T) {
		var got string
		withStdin(t, "2\n", func() {
			_, _, _ = captureStdio(func() error {
				got = promptSelection([]string{"first", "second", "third"})
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
				got = promptSelection([]string{"a", "b"})
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
				got = promptSelection([]string{"a", "b"})
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
				got = promptSelection([]string{"a", "b"})
				return nil
			})
		})
		if got != "" {
			t.Errorf("got %q, want empty on non-numeric input", got)
		}
	})
}
