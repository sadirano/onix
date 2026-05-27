package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPreprocessArgs_MultiCharShorts(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"-ls"}, []string{"--list"}},
		{[]string{"-rm"}, []string{"--remove"}},
		{[]string{"foo", "-rm", "bar"}, []string{"foo", "--remove", "bar"}},
		// Single-rune shorts must NOT be rewritten — kong handles those.
		{[]string{"-l"}, []string{"-l"}},
		{[]string{"-r"}, []string{"-r"}},
		// Long forms unchanged.
		{[]string{"--list"}, []string{"--list"}},
		// Empty pass-through.
		{[]string{}, []string{}},
	}
	for _, c := range cases {
		got := preprocessArgs(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("preprocessArgs(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseRemoveArgs(t *testing.T) {
	cases := []struct {
		name      string
		in        []string
		wantFiles []string
		wantForce bool
		wantRecur bool
		wantErr   bool
	}{
		{"empty", []string{}, nil, false, false, false},
		{"single file", []string{"a.txt"}, []string{"a.txt"}, false, false, false},
		{"force short", []string{"-F", "a.txt"}, []string{"a.txt"}, true, false, false},
		{"force long", []string{"--force", "a.txt"}, []string{"a.txt"}, true, false, false},
		{"recursive short", []string{"-R", "dir"}, []string{"dir"}, false, true, false},
		{"mixed", []string{"-F", "-R", "a", "b"}, []string{"a", "b"}, true, true, false},
		{"unknown flag", []string{"--bogus", "a"}, nil, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files, force, recur, err := parseRemoveArgs(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(files, c.wantFiles) {
				t.Errorf("files = %v, want %v", files, c.wantFiles)
			}
			if force != c.wantForce || recur != c.wantRecur {
				t.Errorf("force=%v recur=%v, want force=%v recur=%v", force, recur, c.wantForce, c.wantRecur)
			}
		})
	}
}

func TestDispatchAlias_BareResolvesAlias(t *testing.T) {
	home := newTestHome(t)
	target := filepath.Join(t.TempDir(), "target")
	_ = os.MkdirAll(target, 0o755)
	if _, _, err := captureStdio(func() error {
		return (&AddCmd{Alias: "foo", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	}); err != nil {
		t.Fatal(err)
	}

	// `onix foo` should print the resolved path on stdout.
	stdout, _, err := captureStdio(func() error {
		return dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}, []string{"foo"}, os.Stdout, os.Stderr)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !samePath(strings.TrimSpace(stdout), target) {
		t.Errorf("stdout = %q, want %q", stdout, target)
	}
}

func TestDispatchSystem_ListNamesFastPath(t *testing.T) {
	home := newTestHome(t)
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "zeta", Path: t.TempDir()}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "alpha", Path: t.TempDir()}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})

	stdout, _, err := captureStdio(func() error {
		return dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}, []string{"--list-names"}, os.Stdout, os.Stderr)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.Fields(stdout)
	if len(got) != 2 || !contains(got, "alpha") || !contains(got, "zeta") {
		t.Errorf("--list-names = %v, want both alpha and zeta", got)
	}
}

func TestDispatchSystem_RemoveRequiresInput(t *testing.T) {
	home := newTestHome(t)
	_, _, err := captureStdio(func() error {
		return dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}, []string{"--remove"}, os.Stdout, os.Stderr)
	})
	if err == nil || !strings.Contains(err.Error(), "requires an alias name or one or more files") {
		t.Errorf("expected ambiguity error, got %v", err)
	}
}

func TestDispatchAlias_RemoveAlias(t *testing.T) {
	home := newTestHome(t)
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "doomed", Path: t.TempDir()}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})

	_, _, err := captureStdio(func() error {
		return dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}, []string{"doomed", "--remove"}, os.Stdout, os.Stderr)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Confirm alias is gone.
	if aliasExists(t, home, "doomed") {
		t.Errorf("alias was not removed from store")
	}
}

func TestDispatchAlias_DeleteFilesInAlias(t *testing.T) {
	home := newTestHome(t)
	target := t.TempDir()
	_ = os.WriteFile(filepath.Join(target, "junk.txt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(target, "keep.txt"), []byte("k"), 0o644)
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "tidy", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})

	_, _, err := captureStdio(func() error {
		return dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin},
			[]string{"tidy", "--remove", "junk.txt", "--force"}, os.Stdout, os.Stderr)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "junk.txt")); err == nil {
		t.Errorf("junk.txt should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Errorf("keep.txt should still exist, got %v", err)
	}
}

func TestDispatchSystem_RefusesLoadBearingFile(t *testing.T) {
	home := newTestHome(t)
	_ = os.WriteFile(filepath.Join(home, "aliases.toml"), []byte("# onix aliases\n"), 0o644)

	// Without --force we should refuse — even with the user passing one
	// other file alongside, the batch is rejected up front (atomic-feeling).
	_, _, err := captureStdio(func() error {
		return dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin},
			[]string{"--remove", "aliases.toml"}, os.Stdout, os.Stderr)
	})
	if err == nil || !strings.Contains(err.Error(), "load-bearing") {
		t.Errorf("expected load-bearing guard error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "aliases.toml")); statErr != nil {
		t.Errorf("aliases.toml was deleted despite guard: %v", statErr)
	}
}

func TestDispatcher_UnknownFlagErrors(t *testing.T) {
	home := newTestHome(t)
	err := dispatchNewGrammar(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}, []string{"--bogus"}, os.Stdout, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected unknown-flag error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func newTestHome(t *testing.T) string {
	t.Helper()
	h := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(h, 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

// aliasExists reports whether the named alias is registered. Routed
// through ListCmd's JSON output so the test stays independent of the
// internal/store package shape.
func aliasExists(t *testing.T, home, name string) bool {
	t.Helper()
	stdout, _, err := captureStdio(func() error {
		return (&ListCmd{}).Run(context.Background(), &env{Home: home, JSON: true, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return strings.Contains(stdout, `"name": "`+name+`"`)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestAtoi(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"42", 42, false},
		{" 42 ", 42, false},
		{"-7", -7, false},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := atoi(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("atoi(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("atoi(%q) = %v, want %v", tt.in, got, tt.want)
		}
		if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.in) {
			t.Errorf("error message %q should contain input %q", err.Error(), tt.in)
		}
	}
}

func TestRunStatsFromArgs(t *testing.T) {
	home := newTestHome(t)
	e := &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	ctx := context.Background()

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"flags", []string{"--full", "--cold"}, false},
		{"since space", []string{"--since", "30d"}, false},
		{"since equals", []string{"--since=30d"}, false},
		{"since missing value", []string{"--since"}, true},
		{"top space", []string{"--top", "5"}, false},
		{"top equals", []string{"--top=5"}, false},
		{"top missing value", []string{"--top"}, true},
		{"top bad value", []string{"--top", "abc"}, true},
		{"unknown flag", []string{"--bogus"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runStatsFromArgs(ctx, e, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("runStatsFromArgs() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDispatchSystem(t *testing.T) {
	home := newTestHome(t)
	e := &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	ctx := context.Background()

	t.Run("list-names", func(t *testing.T) {
		err := dispatchSystem(ctx, e, "list-names", nil, os.Stdout, os.Stderr)
		if err != nil {
			t.Errorf("list-names: %v", err)
		}
	})

	t.Run("init happy", func(t *testing.T) {
		newHome := filepath.Join(t.TempDir(), "newhome")
		err := dispatchSystem(ctx, &env{Home: newHome, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}, "init", []string{"--skip-profile"}, os.Stdout, os.Stderr)
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		if _, err := os.Stat(filepath.Join(newHome, "config.toml")); err != nil {
			t.Errorf("config.toml not created: %v", err)
		}
	})

	t.Run("init unknown flag", func(t *testing.T) {
		err := dispatchSystem(ctx, e, "init", []string{"--bogus"}, os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("expected unknown flag error, got %v", err)
		}
	})

	t.Run("bad verb", func(t *testing.T) {
		err := dispatchSystem(ctx, e, "bogus", nil, os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "unknown system action") {
			t.Errorf("expected unknown action error, got %v", err)
		}
	})
}

func TestPrintUsage(t *testing.T) {
	stdout, _, err := captureStdio(func() error {
		printUsage(os.Stdout)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "USAGE:") {
		t.Error("usage output missing 'USAGE:' label")
	}
	if !strings.Contains(stdout, "ALIAS ACTIONS:") {
		t.Error("usage output missing 'ALIAS ACTIONS:' label")
	}
}
