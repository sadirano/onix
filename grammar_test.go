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
		name        string
		in          []string
		wantFiles   []string
		wantForce   bool
		wantRecur   bool
		wantErr     bool
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
		return (&AddCmd{Alias: "foo", Path: target}).Run(context.Background(), &env{Home: home})
	}); err != nil {
		t.Fatal(err)
	}

	// `onix foo` should print the resolved path on stdout.
	stdout, _, err := captureStdio(func() error {
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home}, []string{"foo"})
		if !handled {
			t.Fatal("dispatcher should have handled bare alias")
		}
		return e
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !samePath(strings.TrimSpace(stdout), target) {
		t.Errorf("stdout = %q, want %q", stdout, target)
	}
}

func TestDispatchAlias_AddWithMetadata(t *testing.T) {
	home := newTestHome(t)
	target := filepath.Join(t.TempDir(), "target")

	// `onix foo <path> -d "desc" -o me -t a -t b` — full add form.
	args := []string{"foo", target, "-d", "desc", "-o", "me", "-t", "a", "-t", "b"}
	if _, _, err := captureStdio(func() error {
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home}, args)
		if !handled {
			t.Fatal("dispatcher should have handled add form")
		}
		return e
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Verify the store reflects all metadata fields.
	a := mustLoadAlias(t, home, "foo")
	if a.Description != "desc" || a.Owner != "me" {
		t.Errorf("description/owner mismatch: %+v", a)
	}
	if len(a.Tags) != 2 || a.Tags[0] != "a" || a.Tags[1] != "b" {
		t.Errorf("tags = %v, want [a b]", a.Tags)
	}
}

func TestDispatchSystem_ListNamesFastPath(t *testing.T) {
	home := newTestHome(t)
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "zeta", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	})
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "alpha", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	})

	stdout, _, err := captureStdio(func() error {
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home}, []string{"--list-names"})
		if !handled {
			t.Fatal("dispatcher should have handled --list-names")
		}
		return e
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
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home}, []string{"--remove"})
		if !handled {
			t.Fatal("dispatcher should have handled --remove")
		}
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "requires an alias name or one or more files") {
		t.Errorf("expected ambiguity error, got %v", err)
	}
}

func TestDispatchAlias_RemoveAlias(t *testing.T) {
	home := newTestHome(t)
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "doomed", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	})

	_, _, err := captureStdio(func() error {
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home}, []string{"doomed", "--remove"})
		if !handled {
			t.Fatal("dispatcher should have handled --remove on alias")
		}
		return e
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Confirm alias is gone.
	if _, ok := mustLoadAliasOK(t, home, "doomed"); ok {
		t.Errorf("alias was not removed from store")
	}
}

func TestDispatchAlias_DeleteFilesInAlias(t *testing.T) {
	home := newTestHome(t)
	target := t.TempDir()
	_ = os.WriteFile(filepath.Join(target, "junk.txt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(target, "keep.txt"), []byte("k"), 0o644)
	_, _, _ = captureStdio(func() error {
		return (&AddCmd{Alias: "tidy", Path: target}).Run(context.Background(), &env{Home: home})
	})

	_, _, err := captureStdio(func() error {
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home},
			[]string{"tidy", "--remove", "junk.txt", "--force"})
		if !handled {
			t.Fatal("dispatcher should have handled alias file delete")
		}
		return e
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
	_ = os.WriteFile(filepath.Join(home, "aliases.toml"), []byte("version = 2\n"), 0o644)

	// Without --force we should refuse — even with the user passing one
	// other file alongside, the batch is rejected up front (atomic-feeling).
	_, _, err := captureStdio(func() error {
		handled, e := tryDispatchNewGrammar(context.Background(), &env{Home: home},
			[]string{"--remove", "aliases.toml"})
		if !handled {
			t.Fatal("dispatcher should have handled system --remove")
		}
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "load-bearing") {
		t.Errorf("expected load-bearing guard error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "aliases.toml")); statErr != nil {
		t.Errorf("aliases.toml was deleted despite guard: %v", statErr)
	}
}

func TestDispatcher_LegacySubcommandFallsThrough(t *testing.T) {
	// `onix resolve foo` is the legacy invocation. The new dispatcher
	// must not claim it — kong owns the hot path.
	home := newTestHome(t)
	handled, _ := tryDispatchNewGrammar(context.Background(), &env{Home: home},
		[]string{"resolve", "foo"})
	if handled {
		t.Errorf("legacy `resolve` subcommand should fall through to kong, but was handled")
	}
	handled, _ = tryDispatchNewGrammar(context.Background(), &env{Home: home},
		[]string{"plugin", "list"})
	if handled {
		t.Errorf("`plugin` subcommand should fall through to kong, but was handled")
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

func mustLoadAlias(t *testing.T, home, name string) storeAliasShim {
	t.Helper()
	a, ok := mustLoadAliasOK(t, home, name)
	if !ok {
		t.Fatalf("alias %q not found", name)
	}
	return a
}

type storeAliasShim struct {
	Description string
	Owner       string
	Tags        []string
}

func mustLoadAliasOK(t *testing.T, home, name string) (storeAliasShim, bool) {
	t.Helper()
	// Read aliases.toml via the existing ListCmd JSON path to keep the
	// test independent of internal/store package shape.
	stdout, _, err := captureStdio(func() error {
		return (&ListCmd{}).Run(context.Background(), &env{Home: home, JSON: true})
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Crude extraction — we don't need a full JSON parse for these
	// asserts, just the fields we wrote.
	want := `"name": "` + name + `"`
	if !strings.Contains(stdout, want) {
		return storeAliasShim{}, false
	}
	a := storeAliasShim{
		Description: extractString(stdout, `"description": "`),
		Owner:       extractString(stdout, `"owner": "`),
	}
	if tags := extractTagSlice(stdout); tags != nil {
		a.Tags = tags
	}
	return a, true
}

func extractString(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func extractTagSlice(s string) []string {
	const marker = `"tags": [`
	i := strings.Index(s, marker)
	if i < 0 {
		return nil
	}
	rest := s[i+len(marker):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return nil
	}
	tags := []string{}
	for _, tok := range strings.Split(rest[:end], ",") {
		tok = strings.TrimSpace(tok)
		tok = strings.Trim(tok, `"`)
		if tok != "" {
			tags = append(tags, tok)
		}
	}
	return tags
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
