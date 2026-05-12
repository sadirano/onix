package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// starterAliases is the placeholder aliases.toml written on first init.
// We keep an explicit file (with just a comment) so `onix list` and the
// hot path read the same "empty" state instead of falling through to the
// not-found branch on every invocation until the first `onix add`.
const starterAliases = `# onix aliases — edit with care, prefer 'onix add' / 'onix rm'
`

// starterConfig is the placeholder config.toml written on first init.
// Empty (just a comment + a worked example) so the file exists for the
// user to extend, but no actions are declared yet — so the snippet has
// only the built-in functions.
const starterConfig = `# onix configuration — declare custom actions here.
# After editing, run: onix install-actions
#
# Example:
#
#   [[actions]]
#   name = "test"
#   exec = "go"
#   args = ["test", "./..."]
#
#   [[actions]]
#   name = "pr"
#   exec = "gh"
#   args = ["pr", "view", "{extras}", "--web"]
`

// snippetTemplate is the body of the generated snippet, minus the
// $global:onixExe header (which is computed per-install). We embed the
// absolute path of the running onix binary so the shell functions are
// pinned to the exact binary that generated them — otherwise a stale v1
// onix on PATH would intercept every call and the user would get cryptic
// errors instead of the new behaviour.
//
// We use $global:onixExe (rather than $onixExe) so the variable is
// reachable from any scope, including the script-block context that
// Register-ArgumentCompleter creates for the completer.
const snippetTemplate = `$onixAliasCompleter = {
    param($wordToComplete, $commandAst, $cursorPosition)
    @(& $global:onixExe list-names 2>$null) |
        Where-Object { $_ -like "$wordToComplete*" } |
        ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
}

function global:o {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$false)][string]$Alias,
        [Parameter(Position=1, Mandatory=$false)][string]$Path
    )
    if (-not $Alias) {
        & $global:onixExe aliases
        return
    }
    if ($Path) {
        # 'o foo C:\some\path' — register (or update) the alias and cd
        # into it. The directory is auto-created by 'onix add' if it
        # doesn't exist.
        $resolved = & $global:onixExe add $Alias $Path
    } else {
        $resolved = & $global:onixExe resolve $Alias
    }
    if ($LASTEXITCODE -eq 0) {
        Set-Location -LiteralPath $resolved
    }
}

function global:n {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe edit $Alias
}

function global:s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe explore $Alias
}

function global:y {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe yank $Alias
}

function global:r {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, Mandatory=$true, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe run $Alias -- @Rest
}

`

const bashSnippetTemplate = `o() {
    if [ -z "$1" ]; then
        "$ONIX_EXE" aliases
        return
    fi
    local path
    if [ -n "$2" ]; then
        # 'o foo /some/path' — register (or update) the alias and cd into
        # it. The directory is auto-created by 'onix add' if missing.
        path=$("$ONIX_EXE" add "$1" "$2")
    else
        path=$("$ONIX_EXE" resolve "$1")
    fi
    if [ $? -eq 0 ]; then
        cd "$path"
    fi
}

n() { "$ONIX_EXE" edit "$1"; }
s() { "$ONIX_EXE" explore "$1"; }
y() { "$ONIX_EXE" yank "$1"; }
r() {
    local alias=$1
    shift
    "$ONIX_EXE" run "$alias" -- "$@"
}

# Tab completion. We read names one per line into an array rather than
# relying on word-splitting on $(...), so aliases containing odd
# characters (in practice ValidateAliasName rejects them, but defence in
# depth is cheap) don't get split into multiple completion entries.
# Zsh uses compdef, which requires the user to have called compinit in
# .zshrc beforehand — we gate on its availability so users without
# compinit just lose completion instead of seeing an error on every
# shell start. The zsh body uses portable 'while read' rather than zsh
# parameter flags so bash can parse the function definition at source
# time even though bash never runs it.
if [ -n "$BASH_VERSION" ]; then
    _onix_completer() {
        local cur=${COMP_WORDS[COMP_CWORD]}
        local names
        mapfile -t names < <("$ONIX_EXE" list-names 2>/dev/null)
        COMPREPLY=( $(compgen -W "${names[*]}" -- "$cur") )
    }
elif [ -n "$ZSH_VERSION" ] && command -v compdef >/dev/null 2>&1; then
    _onix_zsh_completer() {
        local line names=()
        while IFS= read -r line; do
            names+=("$line")
        done < <("$ONIX_EXE" list-names 2>/dev/null)
        compadd -- "${names[@]}"
    }
fi

`

// writeShellSnippet regenerates the host-platform shell snippet from the
// current config. Always rewrites the file in full — this is generated
// content and any user-side customisation belongs in the shell profile
// wrapping our functions, not in this file. We accept the actions and
// plugins lists as parameters rather than reloading config here so callers
// can validate first and fail before clobbering the snippet.
//
// The generated snippet hard-codes the absolute path of the currently
// running onix binary. Without this pin, a v1 onix on PATH would silently
// intercept every shell-function call, leading to opaque errors. The
// downside is that moving the binary requires `onix install-actions` to
// regenerate; we trust that's a rare event compared to PATH ambiguity.
//
// On Windows we write onix.ps1; on Linux/macOS we write onix.sh. We
// deliberately avoid writing both — a pin baked into a PowerShell snippet
// on Linux points at a binary the shell can't run, which is just noise
// for `onix doctor` and a footgun for users who copy ~/.onix between
// machines.
func writeShellSnippet(home string, actions []Action, plugins []Plugin) error {
	if runtime.GOOS == "windows" {
		return writePwshShellSnippet(home, actions, plugins)
	}
	return writeBashShellSnippet(home, actions, plugins)
}

func writePwshShellSnippet(home string, actions []Action, plugins []Plugin) error {
	path := shellPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	exe, err := resolveOnixExe()
	if err != nil {
		return err
	}

	var b strings.Builder
	// Header: identification + the pinned binary path. Single-quoted so
	// backslashes in Windows paths stay literal; `''` escapes any quote
	// inside the path itself.
	b.WriteString("# onix shell integration (generated; do not edit — run 'onix install-actions')\n")
	fmt.Fprintf(&b, "# Source from $PROFILE: . '%s'\n\n", strings.ReplaceAll(shellPath(home), `'`, `''`))
	fmt.Fprintf(&b, "$global:onixExe = '%s'\n\n", strings.ReplaceAll(exe, `'`, `''`))

	// Body: completer + built-in functions.
	b.WriteString(snippetTemplate)

	// Custom actions. Order matches config.toml so users can read top-to-bottom.
	for _, a := range actions {
		writeActionFunction(&b, a)
	}

	// Plugin wrappers. Each plugin gets a wrapper named after the plugin
	// itself; multi-entry plugins additionally get one wrapper per entry.
	for _, p := range plugins {
		writePluginFunction(&b, p.Name, p.Name, "")
		for _, e := range p.Entries {
			writePluginFunction(&b, e.EffectiveCmd(), p.Name, e.Name)
		}
	}

	// Register-ArgumentCompleter once for all alias-taking commands. The
	// list is built dynamically so newly-declared actions and plugins get
	// completion for free without any further user action.
	writeCompleterRegistration(&b, actions, plugins)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeBashShellSnippet(home string, actions []Action, plugins []Plugin) error {
	path := bashShellPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	exe, err := resolveOnixExe()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# onix shell integration (generated; do not edit — run 'onix install-actions')\n")
	fmt.Fprintf(&b, "export ONIX_EXE='%s'\n\n", exe)

	// Body: completer + built-in functions.
	b.WriteString(bashSnippetTemplate)

	// Custom actions.
	for _, a := range actions {
		writeActionFunctionBash(&b, a)
	}

	// Plugin wrappers.
	for _, p := range plugins {
		writePluginFunctionBash(&b, p.Name, p.Name, "")
		for _, e := range p.Entries {
			writePluginFunctionBash(&b, e.EffectiveCmd(), p.Name, e.Name)
		}
	}

	writeCompleterRegistrationBash(&b, actions, plugins)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// regenerateShellSnippet loads the current config + plugin state from
// disk and writes a fresh snippet from them. Centralised so init,
// install-actions, plugin add/update/remove all behave identically; the
// alternative would be three subtly different code paths each handling
// the load order their own way.
func regenerateShellSnippet(home string) error {
	cfg, err := LoadConfig(home)
	if err != nil {
		return err
	}
	pf, err := LoadPlugins(home)
	if err != nil {
		return err
	}
	return writeShellSnippet(home, cfg.Actions, pf.Plugins)
}

// resolveOnixExe returns the absolute path of the currently running binary,
// which gets embedded in the snippet so shell functions invoke us directly
// instead of going through PATH. Wrapped so tests can mock it cleanly when
// we add that layer.
func resolveOnixExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", fmt.Errorf("absolutise binary path: %w", err)
	}
	return abs, nil
}

// writeActionFunction emits one custom-action wrapper. We use the same
// shape as `r`: a positional $Alias plus a remaining-args $Rest, dispatched
// through `onix exec <name>`. The "-- @Rest" form mirrors RunCmd's
// passthrough handling so quoting is identical across all dispatch paths.
// $global:onixExe is the pinned binary path injected at the top of the
// snippet — see writeShellSnippet for why we don't just say "onix".
func writeActionFunction(b *strings.Builder, a Action) {
	fmt.Fprintf(b, `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe exec %s $Alias -- @Rest
}

`, a.Name, a.Name)
}

// writePluginFunction emits a plugin wrapper.
//
//	wrapperName — the user-facing function (e.g. "tts", "timer", "t-start").
//	pluginName  — the plugin's `name` in plugins.toml; what plugin-exec
//	              uses to find the binary and config.
//	entryName   — empty for the plugin's primary command; the entry's
//	              `name` (not its `cmd`) for multi-entry wrappers. Carried
//	              into the plugin process as ONIX_ENTRY.
//
// All three are positional in the generated plugin-exec call. Keeping them
// positional (vs flags) means the dispatcher's parser stays dumb: read
// argv[0..2] without any flag parsing.
func writePluginFunction(b *strings.Builder, wrapperName, pluginName, entryName string) {
	fmt.Fprintf(b, `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe plugin-exec %s %q $Alias -- @Rest
}

`, wrapperName, pluginName, entryName)
}

// writeCompleterRegistration emits the single Register-ArgumentCompleter
// line covering every alias-taking function. We always include the five
// built-ins so a config with no custom actions or plugins still gets
// completion. Plugin wrappers (main + every entry) join the list.
func writeCompleterRegistration(b *strings.Builder, actions []Action, plugins []Plugin) {
	names := []string{"o", "n", "s", "y", "r"}
	for _, a := range actions {
		names = append(names, a.Name)
	}
	for _, p := range plugins {
		names = append(names, p.Name)
		for _, e := range p.Entries {
			names = append(names, e.EffectiveCmd())
		}
	}
	fmt.Fprintf(b, "Register-ArgumentCompleter -CommandName %s -ParameterName Alias -ScriptBlock $onixAliasCompleter\n",
		strings.Join(names, ","))
}

func writeActionFunctionBash(b *strings.Builder, a Action) {
	fmt.Fprintf(b, `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" exec %s "$alias" -- "$@"
}

`, a.Name, a.Name)
}

func writePluginFunctionBash(b *strings.Builder, wrapperName, pluginName, entryName string) {
	fmt.Fprintf(b, `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" plugin-exec %s %q "$alias" -- "$@"
}

`, wrapperName, pluginName, entryName)
}

func writeCompleterRegistrationBash(b *strings.Builder, actions []Action, plugins []Plugin) {
	names := []string{"o", "n", "s", "y", "r"}
	for _, a := range actions {
		names = append(names, a.Name)
	}
	for _, p := range plugins {
		names = append(names, p.Name)
		for _, e := range p.Entries {
			names = append(names, e.EffectiveCmd())
		}
	}
	// The zsh branch only fires when compdef exists — see the matching
	// guard around _onix_zsh_completer in bashSnippetTemplate. Without
	// this guard, users without compinit in .zshrc would see a
	// "compdef: command not found" warning on every shell start.
	fmt.Fprintf(b, `if [ -n "$BASH_VERSION" ]; then
    complete -F _onix_completer %s
elif [ -n "$ZSH_VERSION" ] && command -v compdef >/dev/null 2>&1; then
    compdef _onix_zsh_completer %s
fi
`, strings.Join(names, " "), strings.Join(names, " "))
}

// InitCmd creates ~/.onix and installs PowerShell shell integration.
//
// Steps in order: directory tree, snippet (built-ins + whatever custom
// actions are already declared in config.toml), starter data files,
// $PROFILE source line. The last step is opt-out via --skip-profile so
// CI and the smoke script don't touch the developer's real profile.
type InitCmd struct {
	SkipProfile bool `help:"Don't modify the PowerShell $PROFILE." name:"skip-profile"`
}

func (c *InitCmd) Run(e *env) error {
	// 1. directory tree
	if err := os.MkdirAll(filepath.Join(e.Home, "shell"), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", e.Home, err)
	}

	// 2. config + aliases starters — only write if missing, so re-running
	// init is non-destructive. We need config.toml to exist before writing
	// the snippet so users can immediately edit it.
	cfgPath := configPath(e.Home)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfgPath, []byte(starterConfig), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", cfgPath, err)
		}
	}
	aliases := aliasesPath(e.Home)
	if _, err := os.Stat(aliases); os.IsNotExist(err) {
		if err := os.WriteFile(aliases, []byte(starterAliases), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", aliases, err)
		}
	}

	// 3. Regenerate the snippet from the (possibly empty) config + plugins.
	// We go through regenerateShellSnippet so init shares one code path
	// with install-actions and the plugin commands — any future tweak to
	// snippet contents lives in one place.
	if err := regenerateShellSnippet(e.Home); err != nil {
		return err
	}

	fmt.Printf("onix home: %s\n", e.Home)
	fmt.Printf("shell snippet: %s\n", shellPath(e.Home))

	// 4. $PROFILE wiring.
	if c.SkipProfile {
		fmt.Println("skipped $PROFILE update (re-run without --skip-profile to enable)")
		return nil
	}
	if runtime.GOOS != "windows" {
		return sourceFromBashLike(bashShellPath(e.Home))
	}

	return sourceFromProfile(shellPath(e.Home))
}

// sourceFromBashLike appends a source line to .bashrc and/or .zshrc.
func sourceFromBashLike(snippet string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// We look for both .bashrc and .zshrc. If they exist, we append the source
	// line. This is broader than checking $SHELL but ensures that a user
	// who switches between shells is covered.
	files := []string{".bashrc", ".zshrc"}
	var updated []string
	var found bool

	// We use [ -f ... ] so the rc file doesn't error if onix is uninstalled
	// but the source line remains.
	sourceLine := fmt.Sprintf("[ -f '%s' ] && . '%s'", snippet, snippet)

	for _, f := range files {
		p := filepath.Join(home, f)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		found = true

		existing, _ := os.ReadFile(p)
		if strings.Contains(string(existing), snippet) {
			fmt.Printf("%s already sources %s\n", f, snippet)
			continue
		}

		file, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not open %s: %v\n", p, err)
			continue
		}
		if _, err := fmt.Fprintf(file, "\n# Added by 'onix init'\n%s\n", sourceLine); err != nil {
			file.Close()
			return fmt.Errorf("append to %s: %w", f, err)
		}
		file.Close()
		updated = append(updated, f)
	}

	if len(updated) > 0 {
		fmt.Printf("updated: %s\n", strings.Join(updated, ", "))
		fmt.Println("restart your shell (or source the updated file) to activate o/n/s/r/y")
	} else if !found {
		fmt.Printf("no .bashrc or .zshrc found — add this to your shell rc manually:\n  %s\n", sourceLine)
	}
	return nil
}

// sourceFromProfile appends a `. "<snippet>"` line to PowerShell's $PROFILE
// if it's not already present. We invoke powershell.exe to read $PROFILE
// rather than editing the registry — modifying user-owned config files is
// much less invasive than the v1 PATH-mutation flow.
func sourceFromProfile(snippet string) error {
	out, err := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive",
		"-Command", "$PROFILE.CurrentUserAllHosts").Output()
	if err != nil {
		return fmt.Errorf("locate $PROFILE: %w (add manually: . %q)", err, snippet)
	}
	profilePath := strings.TrimSpace(string(out))
	if profilePath == "" {
		return fmt.Errorf("powershell did not return a profile path (add manually: . %q)", snippet)
	}

	// PowerShell single-quoted strings are literal — backslashes don't need
	// escaping and the only special char is a quote, which doubles as its
	// own escape. Using single quotes here keeps Windows paths legible
	// instead of turning C:\foo into C:\\foo via Go's %q.
	sourceLine := fmt.Sprintf(". '%s'", strings.ReplaceAll(snippet, `'`, `''`))

	// Read existing content (may not exist yet). We look for the snippet
	// path rather than the exact source line so a user who hand-edited the
	// dot-source to use double quotes still trips the "already installed"
	// branch instead of getting a duplicate line.
	existing, _ := os.ReadFile(profilePath)
	if strings.Contains(string(existing), snippet) {
		fmt.Printf("$PROFILE already sources %s\n", snippet)
		return nil
	}

	// Make sure the parent dir exists — first-time PowerShell users won't
	// have a Documents\WindowsPowerShell\ directory yet.
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}

	f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open $PROFILE %s: %w", profilePath, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n# Added by 'onix init'\n%s\n", sourceLine); err != nil {
		return fmt.Errorf("append to $PROFILE: %w", err)
	}
	fmt.Printf("updated $PROFILE: %s\n", profilePath)
	fmt.Println("restart PowerShell (or run: . $PROFILE) to activate o/n/s/r/y and custom actions")
	return nil
}
