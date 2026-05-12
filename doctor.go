package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DoctorCmd runs a quick health check and prints a short report. The goal
// is "fix this and onix works"; we don't try to be exhaustive. Each check
// has a short label, an "ok"/"warn"/"err" status, and a human hint when
// something is wrong.
//
// We deliberately print to stdout (not stderr) so users can pipe to less.
// Non-zero exit only if anything is in the "err" tier.
type DoctorCmd struct{}

type checkResult struct {
	name   string
	status string // "ok" | "warn" | "err"
	detail string
}

func (c *DoctorCmd) Run(e *env) error {
	checks := []checkResult{
		checkHome(e.Home),
		checkAliasesFile(e.Home),
		checkConfigFile(e.Home),
		checkSegmentsFile(e.Home),
		checkPluginsFile(e.Home),
		checkShellSnippet(e.Home),
		checkSnippetPin(e.Home),
		checkOnExePath(e.Home),
		checkEditor(),
	}
	checks = append(checks, checkInstalledPlugins(e.Home)...)
	if runtime.GOOS == "windows" {
		checks = append(checks, checkPowerShellProfile(e.Home))
	} else {
		checks = append(checks, checkBashLikeProfile(e.Home))
	}

	var hadErr bool
	for _, r := range checks {
		if r.name == "" {
			// Empty check results are signals from helpers that "this
			// case is already covered upstream — please skip me." Saves
			// callers from writing if/else around every conditional check.
			continue
		}
		switch r.status {
		case "ok":
			fmt.Printf("  ok   %-22s  %s\n", r.name, r.detail)
		case "warn":
			fmt.Printf("  warn %-22s  %s\n", r.name, r.detail)
		case "err":
			fmt.Printf("  err  %-22s  %s\n", r.name, r.detail)
			hadErr = true
		}
	}
	if hadErr {
		return errors.New("one or more checks failed")
	}
	return nil
}

func checkHome(home string) checkResult {
	fi, err := os.Stat(home)
	if err != nil {
		return checkResult{"home dir", "err", fmt.Sprintf("%s missing — run: onix init", home)}
	}
	if !fi.IsDir() {
		return checkResult{"home dir", "err", fmt.Sprintf("%s exists but is not a directory", home)}
	}
	return checkResult{"home dir", "ok", home}
}

func checkAliasesFile(home string) checkResult {
	p := aliasesPath(home)
	if _, err := os.Stat(p); err != nil {
		return checkResult{"aliases.toml", "warn", fmt.Sprintf("%s missing — run: onix init", p)}
	}
	if _, err := LoadStore(home); err != nil {
		return checkResult{"aliases.toml", "err", err.Error()}
	}
	return checkResult{"aliases.toml", "ok", p}
}

// checkSegmentsFile validates segments.toml parses and reports the
// number of global subdir mappings. Missing is fine (no global registry).
// Bad TOML is a real error since a syntax problem would silently drop
// subdir overrides on every `<seg>@<alias>` resolve.
func checkSegmentsFile(home string) checkResult {
	p := segmentsConfigPath(home)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return checkResult{"segments.toml", "ok", "absent (no global subdirs)"}
		}
		return checkResult{"segments.toml", "warn", fmt.Sprintf("%s: %v", p, err)}
	}
	sf, err := LoadSegments(home)
	if err != nil {
		return checkResult{"segments.toml", "err", err.Error()}
	}
	if len(sf.Subdirs) == 0 && len(sf.Contexts) == 0 {
		return checkResult{"segments.toml", "ok", fmt.Sprintf("%s (no subdirs or contexts)", p)}
	}
	return checkResult{"segments.toml", "ok", fmt.Sprintf("%d subdir(s), %d context(s)", len(sf.Subdirs), len(sf.Contexts))}
}

// checkPluginsFile validates plugins.toml parses and reports the plugin
// count. Like checkConfigFile, a missing file is fine (no plugins
// declared). validation runs against the current config.toml actions so
// collisions surface in doctor instead of waiting for the next install.
func checkPluginsFile(home string) checkResult {
	p := pluginsConfigPath(home)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return checkResult{"plugins.toml", "ok", "absent (no plugins installed)"}
		}
		return checkResult{"plugins.toml", "warn", fmt.Sprintf("%s: %v", p, err)}
	}
	pf, err := LoadPlugins(home)
	if err != nil {
		return checkResult{"plugins.toml", "err", err.Error()}
	}
	cfg, _ := LoadConfig(home)
	var actions []Action
	if cfg != nil {
		actions = cfg.Actions
	}
	if err := validatePlugins(pf, actions); err != nil {
		return checkResult{"plugins.toml", "err", err.Error()}
	}
	if len(pf.Plugins) == 0 {
		return checkResult{"plugins.toml", "ok", fmt.Sprintf("%s (no plugins)", p)}
	}
	return checkResult{"plugins.toml", "ok", fmt.Sprintf("%d plugin(s)", len(pf.Plugins))}
}

// checkInstalledPlugins returns one checkResult per declared plugin:
// confirms the binary exists and notes when a plugin is unpinned (which
// means rebuilds can pick up upstream changes without re-prompting).
// Returns nil when no plugins are declared so doctor doesn't pad the
// output with empty entries.
func checkInstalledPlugins(home string) []checkResult {
	pf, err := LoadPlugins(home)
	if err != nil || len(pf.Plugins) == 0 {
		return nil
	}
	out := make([]checkResult, 0, len(pf.Plugins))
	for _, p := range pf.Plugins {
		label := "plugin:" + p.Name
		bin := pluginBinaryPath(home, p.Repo)
		if _, err := os.Stat(bin); err != nil {
			out = append(out, checkResult{label, "err",
				fmt.Sprintf("binary missing at %s — run: onix plugin update %s", bin, p.Name)})
			continue
		}
		if p.Unpinned {
			out = append(out, checkResult{label, "warn",
				fmt.Sprintf("UNPINNED — `onix plugin update` rebuilds from default branch (binary: %s)", bin)})
			continue
		}
		out = append(out, checkResult{label, "ok", fmt.Sprintf("%s @ %s", bin, shortSHA(p.SHA))})
	}
	return out
}

// shortSHA truncates a commit SHA to 12 chars for display. Long enough
// to be unambiguous in any reasonable repo, short enough to fit on a
// single doctor line without wrapping.
func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// checkConfigFile validates config.toml parses and reports how many
// actions are declared. A missing file is fine (no custom actions); a
// parse error is real — that's what stops `onix exec` from working.
func checkConfigFile(home string) checkResult {
	p := configPath(home)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return checkResult{"config.toml", "ok", "absent (no custom actions)"}
		}
		return checkResult{"config.toml", "warn", fmt.Sprintf("%s: %v", p, err)}
	}
	cfg, err := LoadConfig(home)
	if err != nil {
		return checkResult{"config.toml", "err", err.Error()}
	}
	if len(cfg.Actions) == 0 {
		return checkResult{"config.toml", "ok", fmt.Sprintf("%s (no actions)", p)}
	}
	return checkResult{"config.toml", "ok", fmt.Sprintf("%d action(s)", len(cfg.Actions))}
}

func checkShellSnippet(home string) checkResult {
	p := shellPath(home)
	if runtime.GOOS != "windows" {
		p = bashShellPath(home)
	}
	if _, err := os.Stat(p); err != nil {
		return checkResult{"shell snippet", "warn", fmt.Sprintf("%s missing — run: onix init", p)}
	}
	return checkResult{"shell snippet", "ok", p}
}

// checkSnippetPin verifies the absolute path embedded in the snippet
// ($global:onixExe = '...') still points at a real file. If you move the
// onix binary after install, the pin goes stale and every shell function
// fails with "term not recognised" — the right fix is `onix install-actions`
// from the binary's new location.
func checkSnippetPin(home string) checkResult {
	p := shellPath(home)
	if runtime.GOOS != "windows" {
		p = bashShellPath(home)
	}
	if _, err := os.Stat(p); err != nil {
		// Snippet absence is already reported by checkShellSnippet; skip.
		return checkResult{}
	}
	pin := extractSnippetPin(home)
	if pin == "" {
		return checkResult{"snippet pin", "warn",
			"no binary pin in snippet — run: onix install-actions"}
	}
	if _, err := os.Stat(pin); err != nil {
		return checkResult{"snippet pin", "warn",
			fmt.Sprintf("%s missing — run install-actions from the new location", pin)}
	}
	return checkResult{"snippet pin", "ok", pin}
}

// checkOnExePath reports the relationship between $PATH's onix and the
// pinned binary in the snippet. Three outcomes:
//
//   - onix not on PATH at all — that's fine; the shortcuts use the pinned
//     binary, which works regardless of PATH. Reported as "ok" with a hint.
//   - PATH and pin agree — perfect. Reported as "ok".
//   - PATH and pin disagree — DANGER: typing `onix` directly invokes a
//     different binary than the shell functions do. Reported as "warn".
//
// We pass home explicitly (rather than reading os.Args[0] or similar) so
// the smoke script's isolated home gets checked accurately.
func checkOnExePath(home string) checkResult {
	pin := extractSnippetPin(home)
	pathExe, err := exec.LookPath("onix")
	if err != nil {
		// No onix on PATH. Shortcuts still work via the pinned binary.
		// Mention this as an info detail so users adding scripts know they
		// can opt into having `onix` directly available.
		return checkResult{"onix on PATH", "ok",
			"not on PATH (shortcuts use pinned binary; add to PATH for direct `onix` calls)"}
	}
	if pin == "" || samePath(pathExe, pin) {
		return checkResult{"onix on PATH", "ok", pathExe}
	}
	return checkResult{"onix on PATH", "warn",
		fmt.Sprintf("PATH=%s differs from pinned=%s — type `onix` invokes the PATH binary; shortcuts invoke the pinned one. Run install-actions from the binary you want pinned.",
			pathExe, pin)}
}

func checkEditor() checkResult {
	ed := resolveEditor()
	if _, err := exec.LookPath(ed); err == nil {
		return checkResult{"editor", "ok", ed}
	}
	return checkResult{"editor", "warn", fmt.Sprintf("%s not found on PATH — `onix edit` will fail", ed)}
}

func checkPowerShellProfile(home string) checkResult {
	out, err := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive",
		"-Command", "$PROFILE.CurrentUserAllHosts").Output()
	if err != nil {
		return checkResult{"PowerShell $PROFILE", "warn", "could not query $PROFILE"}
	}
	profile := strings.TrimSpace(string(out))
	if profile == "" {
		return checkResult{"PowerShell $PROFILE", "warn", "no $PROFILE returned"}
	}
	content, err := os.ReadFile(profile)
	if err != nil {
		// Distinguish "doesn't exist yet" (first-time user who hasn't run
		// init without --skip-profile) from "exists but we can't read it"
		// (permissions / locked file). The fix is the same but the
		// diagnosis is different.
		if os.IsNotExist(err) {
			return checkResult{"PowerShell $PROFILE", "warn", fmt.Sprintf("%s does not exist — run: onix init", profile)}
		}
		return checkResult{"PowerShell $PROFILE", "warn", fmt.Sprintf("%s unreadable: %v", profile, err)}
	}
	if !strings.Contains(string(content), filepath.ToSlash(shellPath(home))) &&
		!strings.Contains(string(content), shellPath(home)) {
		return checkResult{"PowerShell $PROFILE", "warn", fmt.Sprintf("does not source %s — run: onix init", shellPath(home))}
	}
	return checkResult{"PowerShell $PROFILE", "ok", profile}
}

func checkBashLikeProfile(home string) checkResult {
	h, err := os.UserHomeDir()
	if err != nil {
		return checkResult{"Bash/Zsh profile", "warn", "could not determine home dir"}
	}
	snippet := bashShellPath(home)
	files := []string{".bashrc", ".zshrc"}
	var found, sourced bool
	for _, f := range files {
		p := filepath.Join(h, f)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		found = true
		if strings.Contains(string(data), snippet) {
			sourced = true
			break
		}
	}
	if !found {
		return checkResult{"Bash/Zsh profile", "warn", "neither .bashrc nor .zshrc found"}
	}
	if !sourced {
		return checkResult{"Bash/Zsh profile", "warn", fmt.Sprintf("no .bashrc/.zshrc sources %s — run: onix init", snippet)}
	}
	return checkResult{"Bash/Zsh profile", "ok", "sourced in .bashrc or .zshrc"}
}

// extractSnippetPin parses the generated shell snippet for the binary pin
// line and returns the path inside the single quotes. Returns "" when the
// file is missing or the pin line isn't present.
//
// We branch on runtime.GOOS rather than reading both snippets: writeShellSnippet
// only writes the host-platform snippet, and reading the other one (if it
// exists from a copied ~/.onix) would yield a pin for the wrong OS.
func extractSnippetPin(home string) string {
	if runtime.GOOS == "windows" {
		return extractPwshSnippetPin(home)
	}
	return extractBashSnippetPin(home)
}

// extractPwshSnippetPin reads $global:onixExe = '<path>' from onix.ps1.
// We trust the snippet's own formatting because it's machine-generated:
// the prefix is exact, single quotes are literal, and `''` is the only
// possible quote escape — so a small string search is fine here.
func extractPwshSnippetPin(home string) string {
	data, err := os.ReadFile(shellPath(home))
	if err != nil {
		return ""
	}
	const prefix = `$global:onixExe = '`
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimPrefix(line, prefix)
		end := strings.LastIndex(rest, "'")
		if end < 0 {
			return ""
		}
		// Reverse the PowerShell single-quote escape (`''` -> `'`).
		return strings.ReplaceAll(rest[:end], `''`, `'`)
	}
	return ""
}

// extractBashSnippetPin reads export ONIX_EXE='<path>' from onix.sh.
func extractBashSnippetPin(home string) string {
	data, err := os.ReadFile(bashShellPath(home))
	if err != nil {
		return ""
	}
	const prefix = `export ONIX_EXE='`
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimPrefix(line, prefix)
		end := strings.LastIndex(rest, "'")
		if end < 0 {
			return ""
		}
		return rest[:end]
	}
	return ""
}

// samePath compares two paths as case-folded absolute forms. Windows is
// case-insensitive on disk but case-preserving in argv, so a plain string
// comparison would give false negatives (e.g. C:\Users\... vs c:\users\...).
func samePath(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		aa = a
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		bb = b
	}
	return strings.EqualFold(aa, bb)
}
