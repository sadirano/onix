package snippet

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// Clink integration: a Lua script dropped into clink's default profile
// directory (%LOCALAPPDATA%\clink), which clink always scans for *.lua.
// The script prepends ~/.onix/bin to PATH inside each cmd.exe session so
// the o/e/s/... wrappers work without global PATH edits, and tab-completes
// alias names for every shortcut command — parity with the PowerShell
// argument completer.

// ClinkDir returns clink's default profile directory. Empty when
// LOCALAPPDATA is unset (non-Windows or a stripped environment), which
// callers treat as "clink integration not applicable".
func ClinkDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, "clink")
}

// InstallClinkLua writes the onix clink script into ClinkDir, creating the
// directory if needed (clink adopts it as its profile dir on first run).
// Returns the written path, or "" when there is nowhere to install.
func InstallClinkLua(home string) (string, error) {
	dir := ClinkDir()
	if dir == "" {
		return "", nil
	}
	return writeClinkLua(dir, home)
}

// RefreshClinkLua rewrites the clink script only if a previous --init
// installed it: --sync regenerates existing artifacts but must not grow
// new global side effects.
func RefreshClinkLua(home string) (path string, refreshed bool, err error) {
	dir := ClinkDir()
	if dir == "" {
		return "", false, nil
	}
	p := filepath.Join(dir, "onix.lua")
	if _, statErr := os.Stat(p); statErr != nil {
		return "", false, nil
	}
	p, err = writeClinkLua(dir, home)
	return p, err == nil, err
}

func writeClinkLua(dir, home string) (string, error) {
	content, err := clinkLua(home)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	p := filepath.Join(dir, "onix.lua")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// clinkLua renders the script with the wrapper dir, binary path, and the
// (possibly renamed via [shortcuts]) command names baked in.
func clinkLua(home string) (string, error) {
	exe, err := resolveOnixExe()
	if err != nil {
		return "", err
	}
	cfg, err := config.LoadConfig(home)
	if err != nil {
		return "", err
	}
	s := config.BuiltinDefaults()
	for k, v := range cfg.Shortcuts {
		if _, ok := s[k]; ok {
			s[k] = v
		}
	}
	names := make([]string, 0, len(s))
	for _, v := range s {
		names = append(names, fmt.Sprintf("%q", v))
	}
	slices.Sort(names)

	// Lua string literals use backslash escapes, so Windows paths need \\.
	esc := func(p string) string { return strings.ReplaceAll(filepath.FromSlash(p), `\`, `\\`) }
	bin := filepath.Join(home, "bin")

	return fmt.Sprintf(`-- onix clink integration (generated; run 'onix --sync' to regenerate).
-- Loaded from clink's profile directory. Prepends the onix wrapper dir to
-- PATH for each cmd.exe session and tab-completes alias names for the
-- shortcut commands, mirroring the PowerShell argument completer.

local bin = "%s"
local path = os.getenv("PATH") or ""
if not string.find(string.lower(path), string.lower(bin), 1, true) then
  os.setenv("PATH", bin .. ";" .. path)
end

-- Alias names come from 'onix --list-names', the same sub-millisecond
-- hot path the PowerShell tab completer uses on every keypress.
local function onix_alias_names()
  local names = {}
  local f = io.popen('"%s" --list-names 2>nul')
  if not f then
    return names
  end
  for line in f:lines() do
    if line ~= "" then
      table.insert(names, line)
    end
  end
  f:close()
  return names
end

for _, name in ipairs({ %s }) do
  clink.argmatcher(name):addarg({ onix_alias_names })
end
`, esc(bin), esc(exe), strings.Join(names, ", ")), nil
}
