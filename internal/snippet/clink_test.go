package snippet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupClinkTest(t *testing.T) (home string) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	OnixExeOverride = `C:\fake\bin\onix.exe`
	t.Cleanup(func() { OnixExeOverride = "" })
	return t.TempDir()
}

func TestInstallClinkLua_Content(t *testing.T) {
	home := setupClinkTest(t)

	p, err := InstallClinkLua(home)
	if err != nil {
		t.Fatalf("InstallClinkLua: %v", err)
	}
	if filepath.Base(p) != "onix.lua" {
		t.Fatalf("unexpected path %q", p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	content := string(data)

	// The session PATH must gain the wrapper dir (Lua-escaped backslashes).
	if !strings.Contains(content, `os.setenv("PATH", bin .. ";" .. path)`) {
		t.Errorf("missing PATH prepend:\n%s", content)
	}
	if !strings.Contains(content, strings.ReplaceAll(filepath.Join(home, "bin"), `\`, `\\`)) {
		t.Errorf("missing escaped wrapper dir:\n%s", content)
	}
	// Completion shells out to the list-names hot path on the pinned exe.
	if !strings.Contains(content, `C:\\fake\\bin\\onix.exe" --list-names`) {
		t.Errorf("missing --list-names completer call:\n%s", content)
	}
	// Every built-in shortcut gets an argmatcher.
	for _, want := range []string{`"o"`, `"e"`, `"s"`, `"y"`, `"p"`, `"r"`, `"sg"`, `"ff"`} {
		if !strings.Contains(content, want) {
			t.Errorf("missing shortcut %s in argmatcher list:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "clink.argmatcher(name)") {
		t.Errorf("missing argmatcher registration:\n%s", content)
	}
}

func TestInstallClinkLua_RenamedShortcut(t *testing.T) {
	home := setupClinkTest(t)
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, []byte("[shortcuts]\ns = \"show\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	p, err := InstallClinkLua(home)
	if err != nil {
		t.Fatalf("InstallClinkLua: %v", err)
	}
	data, _ := os.ReadFile(p)
	content := string(data)
	if !strings.Contains(content, `"show"`) {
		t.Errorf("renamed shortcut missing:\n%s", content)
	}
	// The quote-delimited token "s" must be gone ("sg"/"show" don't match it).
	if strings.Contains(content, `"s"`) {
		t.Errorf("stale built-in name still registered:\n%s", content)
	}
}

func TestInstallClinkLua_NoLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	p, err := InstallClinkLua(t.TempDir())
	if err != nil || p != "" {
		t.Fatalf("expected quiet no-op without LOCALAPPDATA, got p=%q err=%v", p, err)
	}
}

func TestRefreshClinkLua_OnlyWhenInstalled(t *testing.T) {
	home := setupClinkTest(t)

	// Never installed: refresh must not create anything.
	if p, ok, err := RefreshClinkLua(home); ok || err != nil || p != "" {
		t.Fatalf("refresh before install: p=%q ok=%v err=%v", p, ok, err)
	}
	if _, err := os.Stat(filepath.Join(ClinkDir(), "onix.lua")); !os.IsNotExist(err) {
		t.Fatalf("refresh created onix.lua without a prior install")
	}

	// Installed then clobbered: refresh must rewrite it.
	installed, err := InstallClinkLua(home)
	if err != nil {
		t.Fatalf("InstallClinkLua: %v", err)
	}
	if err := os.WriteFile(installed, []byte("-- stale"), 0o644); err != nil {
		t.Fatalf("clobber: %v", err)
	}
	p, ok, err := RefreshClinkLua(home)
	if err != nil || !ok {
		t.Fatalf("refresh after install: ok=%v err=%v", ok, err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "clink.argmatcher") {
		t.Errorf("refresh did not regenerate content: %q", string(data))
	}
}
