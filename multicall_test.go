package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestWrapperName(t *testing.T) {
	tests := []struct {
		argv0 string
		want  string
	}{
		{"o", "o"},
		{"o.exe", "o"},
		{"O.EXE", "o"},
		{`C:\Users\me\.onix\bin\sg.exe`, "sg"},
		{"/usr/local/bin/ff", "ff"},
		{"onix", "onix"},
		{"onix.exe", "onix"},
	}
	for _, tt := range tests {
		if got := wrapperName(tt.argv0); got != tt.want {
			t.Errorf("wrapperName(%q) = %q, want %q", tt.argv0, got, tt.want)
		}
	}
}

func TestInvokedAction(t *testing.T) {
	home := t.TempDir()
	// A remapped shortcut: the explore slot (s) is renamed to "show".
	cfg := "[shortcuts]\ns = \"show\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		argv0      string
		wantAction string
		wantOK     bool
	}{
		{"onix", "", false},
		{"onix.exe", "", false},
		{"o", "navigate", true},
		{"o.exe", "navigate", true},
		{"r", "run", true},
		{"sg", "grep", true},
		{"ff", "find", true},
		{"show", "explore", true}, // resolved via the [shortcuts] remap
		{"s", "explore", true},    // the default name still works
		{"bogus", "", false},
	}
	for _, tt := range tests {
		gotAction, gotOK := invokedAction(home, tt.argv0)
		if gotAction != tt.wantAction || gotOK != tt.wantOK {
			t.Errorf("invokedAction(%q) = (%q, %v), want (%q, %v)",
				tt.argv0, gotAction, gotOK, tt.wantAction, tt.wantOK)
		}
	}
}

func TestDesugarMultiCall(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		args        []string
		wantRewrite []string
		wantAlias   string
		wantNav     bool
	}{
		{"bare navigate opens editor", "navigate", nil, []string{"--edit"}, "", false},
		{"bare action prints usage", "edit", nil, nil, "", false},
		{"navigate dash passthrough", "navigate", []string{"-v"}, []string{"-v"}, "", false},
		{"navigate alias", "navigate", []string{"acme"}, nil, "acme", true},
		{"navigate alias ignores extra", "navigate", []string{"acme", "x"}, nil, "acme", true},
		{"edit alias", "edit", []string{"acme"}, []string{"acme", "--edit"}, "", false},
		{"edit alias with files", "edit", []string{"acme", "a.go", "b.go"}, []string{"acme", "--edit", "a.go", "b.go"}, "", false},
		{"run alias with cmd", "run", []string{"acme", "go", "test"}, []string{"acme", "--run", "go", "test"}, "", false},
		{"grep alias", "grep", []string{"acme", "query"}, []string{"acme", "--grep", "query"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRewrite, gotAlias, gotNav := desugarMultiCall(tt.action, tt.args)
			if gotNav != tt.wantNav || gotAlias != tt.wantAlias {
				t.Errorf("nav/alias = (%v, %q), want (%v, %q)", gotNav, gotAlias, tt.wantNav, tt.wantAlias)
			}
			if !tt.wantNav && !reflect.DeepEqual(gotRewrite, tt.wantRewrite) {
				t.Errorf("rewrite = %#v, want %#v", gotRewrite, tt.wantRewrite)
			}
		})
	}
}

// TestMultiCallDispatch drives run() under wrapper argv[0] names end to end,
// stubbing external commands so the navigation subshell and action desugaring
// are observable without spawning a real interactive shell.
func TestMultiCallDispatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ONIX_HOME", home)
	target := t.TempDir()

	prevLookPath := lookPath
	t.Cleanup(func() { lookPath = prevLookPath })
	lookPath = func(name string) (string, error) { return "C:/fake/" + name, nil }

	// Capture every spawned command so we can assert on the subshell's
	// working directory, and make each one exit immediately.
	var spawned []*exec.Cmd
	prevCmd := execCommand
	prevCtx := execCommandContext
	t.Cleanup(func() {
		execCommand = prevCmd
		execCommandContext = prevCtx
	})
	noopCmd := func() *exec.Cmd {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", "exit 0")
		}
		return exec.Command("true")
	}
	execCommand = func(string, ...string) *exec.Cmd {
		c := noopCmd()
		spawned = append(spawned, c)
		return c
	}
	execCommandContext = func(context.Context, string, ...string) *exec.Cmd {
		c := noopCmd()
		spawned = append(spawned, c)
		return c
	}

	runWrapper := func(argv0 string, args ...string) (int, string, string) {
		var outBuf, errBuf bytes.Buffer
		full := append([]string{argv0}, args...)
		code := run(full, strings.NewReader(""), &outBuf, &errBuf)
		return code, outBuf.String(), errBuf.String()
	}

	if code, _, errOut := runWrapper("onix", "acme", target); code != 0 {
		t.Fatalf("register exit %d: %s", code, errOut)
	}

	t.Run("o navigates by spawning a subshell rooted at the target", func(t *testing.T) {
		spawned = nil
		code, _, errOut := runWrapper("o", "acme")
		if code != 0 {
			t.Fatalf("o acme exit %d: %s", code, errOut)
		}
		if len(spawned) == 0 {
			t.Fatal("expected a subshell to be spawned")
		}
		last := spawned[len(spawned)-1]
		want := target
		if resolved, err := filepath.EvalSymlinks(target); err == nil {
			want = resolved
		}
		got := last.Dir
		if resolved, err := filepath.EvalSymlinks(last.Dir); err == nil {
			got = resolved
		}
		if got != want {
			t.Errorf("subshell Dir = %q, want %q", last.Dir, target)
		}
	})

	t.Run("o with no alias does not spawn a subshell", func(t *testing.T) {
		spawned = nil
		// Bare `o` opens the config editor; it must not navigate.
		code, _, _ := runWrapper("o")
		if code != 0 {
			t.Errorf("bare o exit %d", code)
		}
		for _, c := range spawned {
			// The editor may be spawned, but never with Dir == target.
			if c.Dir == target {
				t.Errorf("bare o unexpectedly navigated to target")
			}
		}
	})

	t.Run("o -v passes through to version", func(t *testing.T) {
		code, out, _ := runWrapper("o", "-v")
		if code != 0 {
			t.Errorf("o -v exit %d", code)
		}
		if !strings.Contains(out, "onix") {
			t.Errorf("o -v output = %q, want it to contain 'onix'", out)
		}
	})

	t.Run("r desugars into the run action", func(t *testing.T) {
		// `r acme <cmd>` -> onix acme --run <cmd>; the stub makes <cmd> a noop.
		code, _, errOut := runWrapper("r", "acme", "whatever")
		if code != 0 {
			t.Errorf("r acme exit %d: %s", code, errOut)
		}
	})

	t.Run("e desugars into the edit action", func(t *testing.T) {
		code, _, errOut := runWrapper("e", "acme")
		if code != 0 {
			t.Errorf("e acme exit %d: %s", code, errOut)
		}
	})
}
