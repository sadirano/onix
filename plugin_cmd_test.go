package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/plugins"
)

// preparePluginSrcDir creates the on-disk shape that PluginAddCmd /
// PluginUpdateCmd expects to find inside SourceDir after gitClone/gitFetch:
// a go.mod (so buildPlugin's "only Go plugins" guard passes) and an optional
// onix.toml (so readPluginManifest returns the supplied entries).
func preparePluginSrcDir(t *testing.T, home, repo string, manifest string) string {
	t.Helper()
	dir := plugins.SourceDir(home, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "onix.toml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// testEnv builds an env pointing at home with discardable streams. JSON is
// off by default; callers that want it flip it after the call returns.
func testEnv(home string) *env {
	return &env{
		Home:   home,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
	}
}

// TestPluginAddCmd_MissingPinError checks the up-front argument validation —
// no exec or filesystem state is needed for this branch.
func TestPluginAddCmd_MissingPinError(t *testing.T) {
	home := t.TempDir()
	cmd := &PluginAddCmd{Repo: "user/repo", Yes: true}
	err := cmd.Run(context.Background(), testEnv(home))
	if err == nil {
		t.Fatal("expected error when neither --sha nor --unpinned set")
	}
	if !strings.Contains(err.Error(), "--sha") {
		t.Errorf("error should mention --sha: %v", err)
	}
}

// TestPluginAddCmd_UnpinnedHappyPath drives the full add flow with --unpinned,
// asserting that plugins.toml ends up with the new entry and the snippet
// file is regenerated.
func TestPluginAddCmd_UnpinnedHappyPath(t *testing.T) {
	home := t.TempDir()
	repo := "sadirano/onix-probe"
	manifest := `[[entry]]
name = "run"
`
	preparePluginSrcDir(t, home, repo, manifest)

	installFakeExec(t)
	// gitClone — single exec call. (SourceDir already exists, but the call
	// still goes through gitClone because we haven't created srcDir/.git.)
	pushFakeResponse("", "", 0)
	// gitCheckout("") => symbolic-ref then reset --hard origin/<branch>.
	pushFakeResponse("main\n", "", 0)
	pushFakeResponse("", "", 0)
	// gitHeadSHA.
	pushFakeResponse("abc123def456\n", "", 0)
	// gitHeadMessage.
	pushFakeResponse("first commit\n", "", 0)
	// buildPlugin.
	pushFakeResponse("", "", 0)

	cmd := &PluginAddCmd{Repo: repo, Unpinned: true, Yes: true}
	if err := cmd.Run(context.Background(), testEnv(home)); err != nil {
		t.Fatalf("PluginAddCmd.Run: %v", err)
	}

	pf, err := plugins.LoadPlugins(home)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(pf.Plugins) != 1 {
		t.Fatalf("plugins.toml has %d entries, want 1", len(pf.Plugins))
	}
	got := pf.Plugins[0]
	if got.Name != "probe" {
		t.Errorf("Name = %q, want probe", got.Name)
	}
	if !got.Unpinned {
		t.Error("Unpinned should be true")
	}
	if got.SHA != "abc123def456" {
		t.Errorf("SHA = %q, want abc123def456", got.SHA)
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "run" {
		t.Errorf("entries = %+v, want one entry named 'run'", got.Entries)
	}

	// Snippet must have been regenerated.
	if _, err := os.Stat(filepath.Join(home, "shell", "onix.ps1")); err != nil {
		t.Errorf("snippet not regenerated: %v", err)
	}
}

// TestPluginAddCmd_PinnedHappyPath does the same as the unpinned case but
// passes --sha, which collapses gitCheckout into a single exec call.
func TestPluginAddCmd_PinnedHappyPath(t *testing.T) {
	home := t.TempDir()
	repo := "sadirano/onix-probe"
	preparePluginSrcDir(t, home, repo, "")

	installFakeExec(t)
	// gitClone, then gitCheckout(sha) (single reset), then gitHeadSHA,
	// gitHeadMessage, buildPlugin.
	for _, resp := range []fakeExecResponse{
		{"", "", 0},
		{"", "", 0},
		{"abc123def456\n", "", 0},
		{"pinned commit\n", "", 0},
		{"", "", 0},
	} {
		pushFakeResponse(resp.stdout, resp.stderr, resp.exit)
	}

	cmd := &PluginAddCmd{Repo: repo, SHA: "abc123def456", Yes: true}
	if err := cmd.Run(context.Background(), testEnv(home)); err != nil {
		t.Fatalf("PluginAddCmd.Run: %v", err)
	}

	pf, _ := plugins.LoadPlugins(home)
	if len(pf.Plugins) != 1 {
		t.Fatalf("plugins.toml has %d entries, want 1", len(pf.Plugins))
	}
	if pf.Plugins[0].Unpinned {
		t.Error("Unpinned should be false for an SHA-pinned install")
	}
}

// TestPluginAddCmd_FetchPath confirms that an existing .git directory takes
// the gitFetch branch instead of gitClone.
func TestPluginAddCmd_FetchPath(t *testing.T) {
	home := t.TempDir()
	repo := "sadirano/onix-probe"
	srcDir := preparePluginSrcDir(t, home, repo, "")
	if err := os.MkdirAll(filepath.Join(srcDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	installFakeExec(t)
	// gitFetch, gitCheckout(sha), gitHeadSHA, gitHeadMessage, buildPlugin.
	for range 5 {
		pushFakeResponse("abc123def456\n", "", 0)
	}

	cmd := &PluginAddCmd{Repo: repo, SHA: "abc123def456", Yes: true}
	if err := cmd.Run(context.Background(), testEnv(home)); err != nil {
		t.Fatalf("PluginAddCmd.Run: %v", err)
	}

	if findCall("git", "fetch", "--depth=50", "origin") == nil {
		t.Errorf("gitFetch not invoked: %v", fakeExecCalls)
	}
	for _, call := range fakeExecCalls {
		if strings.Join(call, " ") == "git clone --depth=50 --no-single-branch sadirano/onix-probe "+srcDir {
			t.Errorf("gitClone was invoked despite existing .git: %v", call)
		}
	}
}

// TestPluginUpdateCmd_NoPlugins is the empty-store branch. No exec needed.
func TestPluginUpdateCmd_NoPlugins(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, err := captureStdio(func() error {
		return (&PluginUpdateCmd{}).Run(context.Background(), &env{
			Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "no plugins installed") {
		t.Errorf("expected 'no plugins installed' message, got %q", combined)
	}
}

// TestPluginUpdateCmd_ShaWithoutName errors before touching exec.
func TestPluginUpdateCmd_ShaWithoutName(t *testing.T) {
	home := t.TempDir()
	// Need at least one plugin so the no-plugins branch doesn't short-circuit.
	pf := &plugins.PluginsFile{Plugins: []plugins.Plugin{{Name: "tts", Repo: "user/onix-tts", SHA: "abc"}}}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}

	err := (&PluginUpdateCmd{SHA: "deadbeef"}).Run(context.Background(), testEnv(home))
	if err == nil {
		t.Fatal("expected error when --sha is set without a name")
	}
	if !strings.Contains(err.Error(), "plugin name") {
		t.Errorf("error should mention the missing name: %v", err)
	}
}

// TestPluginUpdateCmd_UnknownName errors with a clear message.
func TestPluginUpdateCmd_UnknownName(t *testing.T) {
	home := t.TempDir()
	pf := &plugins.PluginsFile{Plugins: []plugins.Plugin{{Name: "tts", Repo: "user/onix-tts", SHA: "abc"}}}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}

	err := (&PluginUpdateCmd{Name: "nope"}).Run(context.Background(), testEnv(home))
	if err == nil {
		t.Fatal("expected error for unknown plugin")
	}
	if !strings.Contains(err.Error(), "unknown plugin") {
		t.Errorf("error should say 'unknown plugin': %v", err)
	}
}

// TestPluginUpdateCmd_UnpinnedHappyPath drives one named unpinned-plugin
// update through the fake exec — no SHA change, no confirmation, no entries
// shift, just a rebuild.
func TestPluginUpdateCmd_UnpinnedHappyPath(t *testing.T) {
	home := t.TempDir()
	repo := "sadirano/onix-probe"
	preparePluginSrcDir(t, home, repo, "")
	pf := &plugins.PluginsFile{Plugins: []plugins.Plugin{{
		Name: "probe", Repo: repo, SHA: "oldhash", Unpinned: true,
	}}}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}

	installFakeExec(t)
	// gitFetch.
	pushFakeResponse("", "", 0)
	// gitCheckout("") -> symbolic-ref + reset.
	pushFakeResponse("main\n", "", 0)
	pushFakeResponse("", "", 0)
	// gitHeadSHA — same hash as on record, so the "newSHA != p.SHA" branch
	// is skipped (no confirm, no manifest reload).
	pushFakeResponse("oldhash\n", "", 0)
	// buildPlugin.
	pushFakeResponse("", "", 0)

	if err := (&PluginUpdateCmd{Name: "probe", Yes: true}).Run(context.Background(), testEnv(home)); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestPluginUpdateCmd_PinnedShaBump exercises the SHA-changed branch:
// gitHeadSHA returns a different hash than what's on record, so the test
// path includes gitHeadMessage, readPluginManifest, and an entry rewrite.
func TestPluginUpdateCmd_PinnedShaBump(t *testing.T) {
	home := t.TempDir()
	repo := "sadirano/onix-probe"
	manifest := `[[entry]]
name = "run"
`
	preparePluginSrcDir(t, home, repo, manifest)
	pf := &plugins.PluginsFile{Plugins: []plugins.Plugin{{
		Name: "probe", Repo: repo, SHA: "oldhash",
	}}}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}

	installFakeExec(t)
	pushFakeResponse("", "", 0)             // gitFetch
	pushFakeResponse("", "", 0)             // gitCheckout(sha=oldhash) -> reset
	pushFakeResponse("newhash123\n", "", 0) // gitHeadSHA
	pushFakeResponse("new commit\n", "", 0) // gitHeadMessage
	pushFakeResponse("", "", 0)             // buildPlugin

	if err := (&PluginUpdateCmd{Name: "probe", Yes: true}).Run(context.Background(), testEnv(home)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	pf2, _ := plugins.LoadPlugins(home)
	if pf2.Plugins[0].SHA != "newhash123" {
		t.Errorf("SHA = %q, want newhash123", pf2.Plugins[0].SHA)
	}
	if len(pf2.Plugins[0].Entries) != 1 || pf2.Plugins[0].Entries[0].Name != "run" {
		t.Errorf("entries not refreshed from manifest: %+v", pf2.Plugins[0].Entries)
	}
}

// TestPluginListCmd_EmptyPlain prints the empty hint to stdout.
func TestPluginListCmd_EmptyPlain(t *testing.T) {
	home := t.TempDir()
	stdout, _, err := captureStdio(func() error {
		return (&PluginListCmd{}).Run(context.Background(), &env{
			Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "no plugins installed") {
		t.Errorf("expected empty hint, got %q", stdout)
	}
}

// TestPluginListCmd_EmptyJSON returns an empty JSON array, not the hint.
func TestPluginListCmd_EmptyJSON(t *testing.T) {
	home := t.TempDir()
	stdout, _, err := captureStdio(func() error {
		return (&PluginListCmd{}).Run(context.Background(), &env{
			Home: home, JSON: true, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "[]") {
		t.Errorf("expected JSON empty array, got %q", stdout)
	}
}

// TestPluginListCmd_MissingBinary surfaces the "missing binary" state in the
// human table output when the recorded plugin has no built binary on disk.
func TestPluginListCmd_MissingBinary(t *testing.T) {
	home := t.TempDir()
	pf := &plugins.PluginsFile{Plugins: []plugins.Plugin{{
		Name: "probe", Repo: "user/onix-probe", SHA: "abc",
	}}}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := captureStdio(func() error {
		return (&PluginListCmd{}).Run(context.Background(), &env{
			Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "missing binary") {
		t.Errorf("expected 'missing binary' state, got %q", stdout)
	}
}

// TestPluginListCmd_UnpinnedFlag confirms the (unpinned) suffix appears on
// the state column when the plugin record sets Unpinned=true.
func TestPluginListCmd_UnpinnedFlag(t *testing.T) {
	home := t.TempDir()
	repo := "user/onix-probe"
	// Place a stand-in binary so the state is "installed", then verify the
	// unpinned suffix is appended.
	dir := plugins.SourceDir(home, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugins.BinaryPath(home, repo), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	pf := &plugins.PluginsFile{Plugins: []plugins.Plugin{{
		Name: "probe", Repo: repo, SHA: "abc", Unpinned: true,
	}}}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := captureStdio(func() error {
		return (&PluginListCmd{}).Run(context.Background(), &env{
			Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "(unpinned)") {
		t.Errorf("expected '(unpinned)' marker, got %q", stdout)
	}
}
