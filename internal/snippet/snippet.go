package snippet

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// PwshPath returns home/shell/onix.ps1.
func PwshPath(home string) string {
	return filepath.Join(home, "shell", "onix.ps1")
}

// BashPath returns home/shell/onix.sh.
func BashPath(home string) string {
	return filepath.Join(home, "shell", "onix.sh")
}

const pwshO = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$false)][string]$Alias,
        [Parameter(Position=1, Mandatory=$false)][string]$Path
    )
    if (-not $Alias) {
        & $global:onixExe --edit
        return
    }

    if ($Path) {
        # '%s foo C:\some\path' — register (or update) the alias and cd
        # into it. The directory is auto-created by onix if it doesn't
        # exist yet.
        $resolved = & $global:onixExe $Alias $Path
    } else {
        $resolved = & $global:onixExe $Alias
    }
    if ($LASTEXITCODE -eq 0) {
        Set-Location -LiteralPath $resolved
    }
}
`

const pwshE = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --edit @Rest
}
`

const pwshS = `function global:%s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe $Alias --explore
}
`

const pwshY = `function global:%s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe $Alias --yank
}
`

const pwshR = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, Mandatory=$true, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --run @Rest
}
`

const pwshSG = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --grep @Rest
}
`

const pwshFF = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --find @Rest
}
`

const bashO = `%s() {
    if [ -z "$1" ]; then
        "$ONIX_EXE" --edit
        return
    fi
    local path
    if [ -n "$2" ]; then
        # '%s foo /some/path' — register (or update) the alias and cd into
        # it. The directory is auto-created by onix if missing.
        path=$("$ONIX_EXE" "$1" "$2")
    else
        path=$("$ONIX_EXE" "$1")
    fi
    if [ $? -eq 0 ]; then
        cd "$path"
    fi
}
`

const bashE = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --edit "$@"
}
`

const bashS = `%s() { "$ONIX_EXE" "$1" --explore; }
`

const bashY = `%s() { "$ONIX_EXE" "$1" --yank; }
`

const bashR = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --run "$@"
}
`

const bashSG = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --grep "$@"
}
`

const bashFF = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --find "$@"
}
`

// pwshQ is a tiny convenience: type `q` to exit the shell.
const pwshQ = `function global:q { exit }
`

const pwshCompleter = `$onixAliasCompleter = {
    param($wordToComplete, $commandAst, $cursorPosition)
    @(& $global:onixExe --list-names 2>$null) |
        Where-Object { $_ -like "$wordToComplete*" } |
        ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
}
`

const bashCompleter = `if [ -n "$BASH_VERSION" ]; then
    _onix_completer() {
        local cur=${COMP_WORDS[COMP_CWORD]}
        local names
        mapfile -t names < <("$ONIX_EXE" --list-names 2>/dev/null)
        COMPREPLY=( $(compgen -W "${names[*]}" -- "$cur") )
    }
elif [ -n "$ZSH_VERSION" ] && command -v compdef >/dev/null 2>&1; then
    _onix_zsh_completer() {
        local line names=()
        while IFS= read -r line; do
            names+=("$line")
        done < <("$ONIX_EXE" --list-names 2>/dev/null)
        compadd -- "${names[@]}"
    }
fi
`

// WriteShellSnippet regenerates the host-platform shell snippet.
func WriteShellSnippet(home string, shortcuts map[string]string, actions []config.Action) error {
	if runtime.GOOS == "windows" {
		return WritePwshShellSnippet(home, shortcuts, actions)
	}
	return WriteBashShellSnippet(home, shortcuts, actions)
}

func WritePwshShellSnippet(home string, shortcuts map[string]string, actions []config.Action) error {
	path := PwshPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	exe, err := resolveOnixExe()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# onix shell integration (generated; do not edit — run 'onix sync')\n")
	fmt.Fprintf(&b, "# Source from $PROFILE: . '%s'\n\n", strings.ReplaceAll(path, `'`, `''`))
	fmt.Fprintf(&b, "$global:onixExe = '%s'\n\n", strings.ReplaceAll(exe, `'`, `''`))

	b.WriteString(pwshCompleter)
	b.WriteString("\n")

	s := config.BuiltinDefaults()
	for k, v := range shortcuts {
		if _, ok := s[k]; ok {
			s[k] = v
		}
	}

	fmt.Fprintf(&b, pwshO, s["o"], s["o"])
	fmt.Fprintf(&b, pwshE, s["e"])
	fmt.Fprintf(&b, pwshS, s["s"])
	fmt.Fprintf(&b, pwshY, s["y"])
	fmt.Fprintf(&b, pwshR, s["r"])
	fmt.Fprintf(&b, pwshSG, s["sg"])
	fmt.Fprintf(&b, pwshFF, s["ff"])
	b.WriteString(pwshQ)
	b.WriteString("\n")

	// On Windows, we also drop .cmd wrappers into ~/.onix/bin for each
	// shortcut and custom action. This makes them available via Windows
	// Run (Win+R) or from cmd.exe without needing the PowerShell snippet.
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)

	writeOCmdWrapper(binDir, exe, s["o"])
	writeFindPreviewWrapper(binDir)
	writeAliasFlagWrapper(binDir, exe, s["e"], "--edit")
	writeExploreWrapper(binDir, s["r"], s["s"])
	writeAliasFlagWrapper(binDir, exe, s["y"], "--yank")
	writeAliasFlagWrapper(binDir, exe, s["r"], "--run")
	writeAliasFlagWrapper(binDir, exe, s["sg"], "--grep")
	writeAliasFlagWrapper(binDir, exe, s["ff"], "--find")

	for _, a := range actions {
		writeActionFunction(&b, a)
		writeAliasFlagWrapper(binDir, exe, a.Name, "--exec", a.Name)
	}

	writeCompleterRegistration(&b, s, actions)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// FindPreviewWrapperName is the on-disk filename of the fzf preview
// helper used by `onix find`. Exported so search.go can build its path
// without re-encoding the string.
const FindPreviewWrapperName = "onix-preview.cmd"

// writeFindPreviewWrapper drops a tiny batch helper next to the other
// shims. fzf calls it once per highlighted row: directories get a
// `dir /b` listing, everything else goes through bat. Keeping the
// branching logic in a real script (rather than inline in --preview)
// sidesteps cmd.exe's parser quirks with parens and quoted paths.
func writeFindPreviewWrapper(binDir string) {
	// NB: fzf shell-escapes substituted {} values with carets on Windows,
	// and because we wrap {} in double quotes (so paths with spaces stay
	// one token) cmd.exe does NOT strip those carets — they survive into
	// %~1. We scrub them via delayed-expansion substitution (the regular
	// %p:^=% form is unreliable; cmd's parser treats `^=` as escaped `=`
	// so the pattern collapses and nothing gets stripped).
	//
	// Directory test uses pushd, not `if exist "...\."` — the latter
	// returns true for files on some cmd builds, which is exactly the
	// regression that sent files through the `dir /b` branch.
	const content = `@echo off
setlocal enabledelayedexpansion
set "p=%~1"
set "p=!p:^=!"
pushd "!p!" >nul 2>&1
if errorlevel 1 (
  bat --style=numbers --color=always "!p!"
) else (
  popd
  dir /b "!p!"
)
`
	_ = os.WriteFile(filepath.Join(binDir, FindPreviewWrapperName), []byte(content), 0o644)
}

func writeOCmdWrapper(binDir, exe, name string) {
	path := filepath.Join(binDir, name+".cmd")
	// The 'o' wrapper mimics the PowerShell function's "no-args means
	// aliases" behavior. For alias lookups it 'cd /d's into the target;
	// anything else (subcommands, unknown names) is delegated to onix
	// itself, which dispatches subcommands or prompts to register a new
	// alias. Win+R invocations get a persistent shell via 'cmd /k'.
	//
	// NB: no setlocal. setlocal + cd reverts the working directory when
	// the script exits, which would silently break the whole point of
	// `o`. We use a unique variable name (_onix_target) to minimise
	// collisions with whatever the user has in their shell.
	//
	// NB: %~1 is passed unquoted to the for /f backtick command. When
	// the command starts with a quoted exe path AND contains another
	// quoted token, for /f's usebackq tokenizer mis-parses the trailing
	// quote and the inner command runs with corrupted args (silently —
	// stdout capture returns nothing). Alias names are validated to
	// contain no spaces or shell metachars, so quoting is unnecessary.
	content := fmt.Sprintf(`@echo off
if "%%~1"=="" (
  "%s" --edit
  exit /b
)

rem A leading dash means a system verb (-v, --version, --doctor, ...).
rem Skip the alias-resolve attempt — onix's stdout would otherwise be
rem captured and fed to 'cd' as a bogus path.
set "_onix_arg=%%~1"
if "%%_onix_arg:~0,1%%"=="-" (
  set "_onix_arg="
  "%s" %%*
  exit /b
)
set "_onix_arg="

set "_onix_target="
for /f "usebackq delims=" %%%%i in (`+"`"+`"%s" %%~1 --no-prompt 2^>nul`+"`"+`) do set "_onix_target=%%%%i"
if not defined _onix_target (
  "%s" %%*
  exit /b
)

cd /d "%%_onix_target%%"
set "_onix_target="
if %%0 == "%%~f0" cmd /k
`, exe, exe, exe, exe)
	_ = os.WriteFile(path, []byte(content), 0o644)
}

// writeExploreWrapper emits the explore shim. Instead of going through
// onix --explore (which shells out to explorer.exe and inherits its
// non-zero exit codes and awkward cwd handling), it delegates to the run
// wrapper: resolve the alias's directory, cd there, and launch
// `explorer .`. runName is the run shortcut's wrapper name, which lives
// in the same bin dir and is therefore resolvable on PATH.
func writeExploreWrapper(binDir, runName, name string) {
	path := filepath.Join(binDir, name+".cmd")
	content := fmt.Sprintf("@echo off\r\n%s %%* explorer .\r\n", runName)
	_ = os.WriteFile(path, []byte(content), 0o644)
}

// writeAliasFlagWrapper emits a .cmd shim that translates
//
//	<wrapperName> <alias> [args...]
//
// into the alias-flag invocation
//
//	<onixExe> <alias> <flag> [extras...] [args...]
//
// where `extras` is whatever fixed positionals the flag needs (e.g. an
// action name for --exec). The shim caps
// passthrough at 8 trailing args (%2..%9), which fits every real-world
// use of the wrappers — multi-arg invocations should call onix directly.
func writeAliasFlagWrapper(binDir, exe, name, flag string, extras ...string) {
	path := filepath.Join(binDir, name+".cmd")
	extraStr := ""
	if len(extras) > 0 {
		extraStr = " " + strings.Join(extras, " ")
	}
	content := fmt.Sprintf(
		"@echo off\r\n\"%s\" %%1 %s%s %%2 %%3 %%4 %%5 %%6 %%7 %%8 %%9\r\n",
		exe, flag, extraStr,
	)
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func WriteBashShellSnippet(home string, shortcuts map[string]string, actions []config.Action) error {
	path := BashPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	exe, err := resolveOnixExe()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# onix shell integration (generated; do not edit — run 'onix sync')\n")
	fmt.Fprintf(&b, "export ONIX_EXE='%s'\n\n", exe)

	b.WriteString(bashCompleter)
	b.WriteString("\n")

	s := config.BuiltinDefaults()
	for k, v := range shortcuts {
		if _, ok := s[k]; ok {
			s[k] = v
		}
	}

	fmt.Fprintf(&b, bashO, s["o"], s["o"])
	fmt.Fprintf(&b, bashE, s["e"])
	fmt.Fprintf(&b, bashS, s["s"])
	fmt.Fprintf(&b, bashY, s["y"])
	fmt.Fprintf(&b, bashR, s["r"])
	fmt.Fprintf(&b, bashSG, s["sg"])
	fmt.Fprintf(&b, bashFF, s["ff"])
	b.WriteString("\n")

	for _, a := range actions {
		writeActionFunctionBash(&b, a)
	}

	writeCompleterRegistrationBash(&b, s, actions)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// RegenerateShellSnippet loads the current config and writes a fresh snippet.
func RegenerateShellSnippet(home string) error {
	cfg, err := config.LoadConfig(home)
	if err != nil {
		return err
	}
	return WriteShellSnippet(home, cfg.Shortcuts, cfg.Actions)
}

var OnixExeOverride string

// resolveOnixExe returns the path to the current onix binary.
// Can be overridden by OnixExeOverride for stable tests.
func resolveOnixExe() (string, error) {
	if OnixExeOverride != "" {
		return OnixExeOverride, nil
	}
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

func writeActionFunction(b *strings.Builder, a config.Action) {
	fmt.Fprintf(b, `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --exec %s @Rest
}

`, a.Name, a.Name)
}

func writeCompleterRegistration(b *strings.Builder, shortcuts map[string]string, actions []config.Action) {
	names := make([]string, 0, 7+len(actions))
	for _, v := range shortcuts {
		names = append(names, v)
	}
	for _, a := range actions {
		names = append(names, a.Name)
	}
	slices.Sort(names)
	fmt.Fprintf(b, "Register-ArgumentCompleter -CommandName %s -ParameterName Alias -ScriptBlock $onixAliasCompleter\n",
		strings.Join(names, ","))
}

func writeActionFunctionBash(b *strings.Builder, a config.Action) {
	fmt.Fprintf(b, `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --exec %s "$@"
}

`, a.Name, a.Name)
}

func writeCompleterRegistrationBash(b *strings.Builder, shortcuts map[string]string, actions []config.Action) {
	names := make([]string, 0, 7+len(actions))
	for _, v := range shortcuts {
		names = append(names, v)
	}
	for _, a := range actions {
		names = append(names, a.Name)
	}
	slices.Sort(names)
	fmt.Fprintf(b, `if [ -n "$BASH_VERSION" ]; then
    complete -F _onix_completer %s
elif [ -n "$ZSH_VERSION" ] && command -v compdef >/dev/null 2>&1; then
    compdef _onix_zsh_completer %s
fi
`, strings.Join(names, " "), strings.Join(names, " "))
}
