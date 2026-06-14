package snippet

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestWritePwshShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := WritePwshShellSnippet(dir, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(PwshPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, scrub(string(data), dir), "pwsh-no-actions.ps1.golden")
}

func TestWriteBashShellSnippet_NoActions(t *testing.T) {
	dir := t.TempDir()
	if err := WriteBashShellSnippet(dir, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(BashPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	assertGolden(t, scrub(string(data), dir), "bash-no-actions.sh.golden")
}

func TestWriteShellSnippet_HostPlatformOnly(t *testing.T) {
	dir := t.TempDir()
	if err := WriteShellSnippet(dir, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	pwshExists := fileExists(PwshPath(dir))
	bashExists := fileExists(BashPath(dir))
	if pwshExists == bashExists {
		t.Fatalf("expected exactly one snippet, got pwsh=%v bash=%v", pwshExists, bashExists)
	}
}

// TestWritePwshShellSnippet_InstallsExeWrappers verifies the snippet writer
// installs the multi-call wrappers into bin and that each carries the onix
// binary's bytes (hardlink or copy).
func TestWritePwshShellSnippet_InstallsExeWrappers(t *testing.T) {
	dir := t.TempDir()
	if err := WritePwshShellSnippet(dir, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "bin")

	onixInfo, err := os.Stat(filepath.Join(bin, "onix"+exeExt()))
	if err != nil {
		t.Fatalf("canonical onix binary not installed into bin: %v", err)
	}
	for _, name := range []string{"o", "e", "s", "y", "p", "r", "sg", "ff"} {
		info, err := os.Stat(filepath.Join(bin, name+exeExt()))
		if err != nil {
			t.Errorf("wrapper %q not installed: %v", name, err)
			continue
		}
		if info.Size() != onixInfo.Size() {
			t.Errorf("wrapper %q size %d != onix size %d", name, info.Size(), onixInfo.Size())
		}
	}

	// Wrappers are executables, never batch shims.
	if matches, _ := filepath.Glob(filepath.Join(bin, "*.cmd")); len(matches) != 0 {
		t.Errorf("expected no .cmd wrappers, found %v", matches)
	}
}

// TestWritePwshShellSnippet_RenamedShortcut confirms a [shortcuts] remap names
// the installed wrapper and the completer registration accordingly.
func TestWritePwshShellSnippet_RenamedShortcut(t *testing.T) {
	dir := t.TempDir()
	if err := WritePwshShellSnippet(dir, map[string]string{"s": "show"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "show"+exeExt())); err != nil {
		t.Errorf("renamed wrapper 'show' not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "s"+exeExt())); err == nil {
		t.Errorf("default wrapper 's' should not exist when remapped to 'show'")
	}
	data, _ := os.ReadFile(PwshPath(dir))
	if !strings.Contains(string(data), "show") {
		t.Errorf("completer registration missing renamed command 'show':\n%s", data)
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
