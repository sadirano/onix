package snippet

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/plugins"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestWritePwshShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := WritePwshShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(PwshPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, scrub(string(data), dir), "pwsh-no-actions.ps1.golden")
}

func TestWritePwshShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []config.Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := WritePwshShellSnippet(dir, nil, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(PwshPath(dir))
	assertGolden(t, scrub(string(data), dir), "pwsh-with-actions.ps1.golden")
}

func TestWritePwshShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plgs := []plugins.Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{
			Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []plugins.PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"},
			},
		},
	}
	if err := WritePwshShellSnippet(dir, nil, nil, plgs); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(PwshPath(dir))
	assertGolden(t, scrub(string(data), dir), "pwsh-with-plugins.ps1.golden")
}

func TestWriteBashShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := WriteBashShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(BashPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, scrub(string(data), dir), "bash-no-actions.sh.golden")
}

func TestWriteBashShellSnippet_WithActions(t *testing.T) {
	dir := t.TempDir()
	actions := []config.Action{
		{Name: "test", Exec: "go", Args: []string{"test", "./..."}},
		{Name: "pr", Exec: "gh", Args: []string{"pr", "view", "{extras}"}},
	}
	if err := WriteBashShellSnippet(dir, nil, actions, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(BashPath(dir))
	assertGolden(t, scrub(string(data), dir), "bash-with-actions.sh.golden")
}

func TestWriteBashShellSnippet_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	plgs := []plugins.Plugin{
		{Name: "tts", Repo: "sadirano/onix-tts", SHA: "abc"},
		{
			Name: "timer", Repo: "sadirano/onix-timer", SHA: "def",
			Entries: []plugins.PluginEntry{
				{Name: "start", Cmd: "t-start"},
				{Name: "stop"},
			},
		},
	}
	if err := WriteBashShellSnippet(dir, nil, nil, plgs); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(BashPath(dir))
	assertGolden(t, scrub(string(data), dir), "bash-with-plugins.sh.golden")
}

func TestWriteShellSnippet_HostPlatformOnly(t *testing.T) {
	dir := t.TempDir()
	if err := WriteShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	pwshExists := fileExists(PwshPath(dir))
	bashExists := fileExists(BashPath(dir))
	if pwshExists == bashExists {
		t.Fatalf("expected exactly one snippet, got pwsh=%v bash=%v", pwshExists, bashExists)
	}
}

func TestWritePwshShellSnippet_OCmdWrapper(t *testing.T) {
	dir := t.TempDir()
	if err := WritePwshShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(dir, "bin", "o.cmd")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(data)
	if !strings.Contains(content, "--edit") {
		t.Errorf("o.cmd missing '--edit' for no-arg invocation:\n%s", content)
	}
	if !strings.Contains(content, "--no-prompt") {
		t.Errorf("o.cmd missing '--no-prompt' lookup:\n%s", content)
	}
	if !strings.Contains(content, "if not defined _onix_target") {
		t.Errorf("o.cmd missing subcommand passthrough fallback:\n%s", content)
	}
	// Regression guard: a leading-dash first arg ('-v', '--doctor', ...)
	// must bypass the alias-resolve attempt — otherwise its stdout is
	// captured into _onix_target and fed to 'cd' as a bogus path.
	if !strings.Contains(content, `if "%_onix_arg:~0,1%"=="-"`) {
		t.Errorf("o.cmd missing leading-dash bypass:\n%s", content)
	}
	// Regression guard: setlocal + cd reverts the working directory when
	// the script exits, which would silently break `o`. The wrapper must
	// not use setlocal.
	if strings.Contains(content, "setlocal") {
		t.Errorf("o.cmd must not use 'setlocal' — it would revert cd on script exit:\n%s", content)
	}
}

func TestWritePwshShellSnippet_FindPreviewWrapper(t *testing.T) {
	dir := t.TempDir()
	if err := WritePwshShellSnippet(dir, nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(dir, "bin", FindPreviewWrapperName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(data)
	if !strings.Contains(content, `pushd "!p!"`) {
		t.Errorf("preview wrapper missing pushd directory test:\n%s", content)
	}
	if !strings.Contains(content, "dir /b") || !strings.Contains(content, "bat ") {
		t.Errorf("preview wrapper missing dir/bat branches:\n%s", content)
	}
	// Regression: fzf prefixes substituted {} chars with ^ on Windows,
	// quotes protect them from cmd's normal stripping. We strip via
	// delayed expansion because the plain %p:^=% pattern collapses
	// (cmd treats ^= as escaped =).
	if !strings.Contains(content, "setlocal enabledelayedexpansion") {
		t.Errorf("preview wrapper missing delayed expansion:\n%s", content)
	}
	if !strings.Contains(content, "set \"p=!p:^=!\"") {
		t.Errorf("preview wrapper missing caret strip:\n%s", content)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func assertGolden(t *testing.T, got, goldenName string) {
	t.Helper()
	normalize := func(s string) string {
		return strings.ReplaceAll(s, "\r\n", "\n")
	}
	path := filepath.Join("..", "..", "testdata", "snippet", goldenName)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(normalize(got)), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create)", err)
	}
	want := string(data)
	if normalize(got) != normalize(want) {
		t.Errorf("golden mismatch for %s\nGOT:\n%s\nWANT:\n%s", goldenName, got, want)
	}
}

func scrub(s, tempDir string) string {
	s = strings.ReplaceAll(s, tempDir, "/ONIX_HOME")
	exe, _ := os.Executable()
	if exe != "" {
		s = strings.ReplaceAll(s, exe, "/ONIX_EXE")
	}
	if OnixExeOverride != "" {
		s = strings.ReplaceAll(s, OnixExeOverride, "/ONIX_EXE")
	}
	return strings.ReplaceAll(s, `\`, "/")
}
