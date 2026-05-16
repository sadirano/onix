package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/plugins"
)

func TestResolveRepoURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://github.com/foo/bar", "https://github.com/foo/bar"},
		{"/abs/path", "/abs/path"},
		{"C:/abs/path", "C:/abs/path"},
		{"foo/bar", "https://github.com/foo/bar.git"},
		{"sadirano/onix-tts", "https://github.com/sadirano/onix-tts.git"},
	}
	for _, tc := range tests {
		got := resolveRepoURL(tc.in)
		if got != tc.want {
			t.Errorf("resolveRepoURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadPluginManifest(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "onix.toml")
	content := `
[[entry]]
name = "start"
cmd  = "t-start"

[[entry]]
name = "stop"
`
	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := readPluginManifest(dir)
	if err != nil {
		t.Fatalf("readPluginManifest: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "start" || entries[0].Cmd != "t-start" {
		t.Errorf("entry 0 incorrect: %+v", entries[0])
	}
	if entries[1].Name != "stop" || entries[1].Cmd != "" {
		t.Errorf("entry 1 incorrect: %+v", entries[1])
	}
}

func TestPluginListCmd(t *testing.T) {
	home := t.TempDir()
	// Create plugins.toml
	pf := &plugins.PluginsFile{
		Plugins: []plugins.Plugin{
			{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		},
	}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureStdio(func() error {
		return (&PluginListCmd{}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "tts") {
		t.Errorf("output missing plugin data: %q", stdout)
	}

	// JSON mode
	stdout, _, err = captureStdio(func() error {
		return (&PluginListCmd{}).Run(context.Background(), &env{Home: home, JSON: true})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"Name": "tts"`) {
		t.Errorf("JSON output missing plugin data: %q", stdout)
	}
}

func TestPluginRemoveCmd(t *testing.T) {
	home := t.TempDir()
	// Create plugins.toml with a plugin
	pf := &plugins.PluginsFile{
		Plugins: []plugins.Plugin{
			{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		},
	}
	if err := plugins.SavePlugins(home, pf); err != nil {
		t.Fatal(err)
	}

	// Remove missing plugin
	err := (&PluginRemoveCmd{Name: "nope"}).Run(context.Background(), &env{Home: home})
	if err == nil {
		t.Error("expected error removing missing plugin, got nil")
	}

	// Remove existing plugin
	err = (&PluginRemoveCmd{Name: "tts"}).Run(context.Background(), &env{Home: home})
	if err != nil {
		t.Fatalf("PluginRemoveCmd.Run: %v", err)
	}

	// Verify it's gone
	pf2, _ := plugins.LoadPlugins(home)
	if len(pf2.Plugins) != 0 {
		t.Errorf("plugin still exists after removal: %+v", pf2.Plugins)
	}
}

func TestPluginAddCmd_Local(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	home := t.TempDir()
	tempParent := t.TempDir()
	repoDir := filepath.Join(tempParent, "onix-dummy")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Initialize a dummy plugin repo
	run := func(dir string, name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
		}
	}

	run(repoDir, "git", "init")
	run(repoDir, "git", "config", "user.email", "test@example.com")
	run(repoDir, "git", "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module dummy\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "onix.toml"), []byte("[[entry]]\nname = \"run\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run(repoDir, "git", "add", ".")
	run(repoDir, "git", "commit", "-m", "initial commit")

	shaBytes, _ := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	sha := strings.TrimSpace(string(shaBytes))

	// Run plugin add
	// We use --yes to skip confirmation
	cmd := &PluginAddCmd{
		Repo: repoDir,
		SHA:  sha,
		Yes:  true,
	}
	if err := cmd.Run(context.Background(), &env{Home: home}); err != nil {
		t.Fatalf("PluginAddCmd.Run: %v", err)
	}

	// Verify plugin is installed
	pf, _ := plugins.LoadPlugins(home)
	if len(pf.Plugins) != 1 || pf.Plugins[0].Name != "dummy" {
		t.Errorf("plugin not installed correctly: %+v", pf.Plugins)
	}
}
