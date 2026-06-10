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
        [Parameter(Position=1, Mandatory=$false, ValueFromRemainingArguments=$true)][string[]]$Path
    )
    if (-not $Alias) {
        & $global:onixExe --edit
        return
    }

    $resolved = & $global:onixExe $Alias @Path
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
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --explore @Rest
}
`

const pwshY = `function global:%s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe $Alias --yank
}
`

const pwshP = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe $Alias --paste @Rest
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
    path=$("$ONIX_EXE" "$@")
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

const bashS = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --explore "$@"
}
`

const bashY = `%s() { "$ONIX_EXE" "$1" --yank; }
`

const bashP = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" "$alias" --paste "$@"
}
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
func WriteShellSnippet(home string, shortcuts map[string]string) error {
	if runtime.GOOS == "windows" {
		return WritePwshShellSnippet(home, shortcuts)
	}
	return WriteBashShellSnippet(home, shortcuts)
}

func WritePwshShellSnippet(home string, shortcuts map[string]string) error {
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

	fmt.Fprintf(&b, pwshO, s["o"])
	fmt.Fprintf(&b, pwshE, s["e"])
	fmt.Fprintf(&b, pwshS, s["s"])
	fmt.Fprintf(&b, pwshY, s["y"])
	fmt.Fprintf(&b, pwshP, s["p"])
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
	writeRegisterWrapper(binDir, exe)
	writeFindPreviewWrapper(binDir)
	writeAliasFlagWrapper(binDir, exe, s["e"], "--edit")
	writeExploreWrapper(binDir, exe, s["r"], s["s"])
	writeAliasFlagWrapper(binDir, exe, s["y"], "--yank")
	writeAliasFlagWrapper(binDir, exe, s["p"], "--paste")
	writeAliasFlagWrapper(binDir, exe, s["r"], "--run")
	writeAliasFlagWrapper(binDir, exe, s["sg"], "--grep")
	writeAliasFlagWrapper(binDir, exe, s["ff"], "--find")

	writeCompleterRegistration(&b, s)

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

// writeOCmdWrapper emits the navigation shim used from cmd.exe and Win+R.
// With no argument it opens the config editor; a leading dash is a system
// verb handed straight to onix; anything else is an alias to navigate to.
//
// The resolved path is captured by redirecting onix's stdout into the
// ~/.onix/.last file, which the wrapper then reads to pushd into. A child
// process can't relocate its parent shell, so this is how the .cmd flow cds
// the calling shell. An unknown alias (and not an @-segment) falls back to
// register.cmd, the Everything + fzf directory picker.
func writeOCmdWrapper(binDir, exe, name string) {
	path := filepath.Join(binDir, name+".cmd")
	content := fmt.Sprintf(`@echo off
:: onix navigation wrapper (generated; run 'onix --sync' to regenerate).
:: Resolve an alias and cd the current shell into its directory; with no
:: argument it opens the config editor instead.

set "ONIX_EXE=%s"
set "ONIX_LAST_FILE=%%~dp0\..\.last"

:: No arguments: open the editor and stop.
if "%%~1"=="" (
  "%%ONIX_EXE%%" --edit
  exit /b
)

:: A leading dash marks a system verb (-v, --version, ...). Pass it straight
:: through to onix rather than treating it as a navigation alias.
set "_arg=%%~1"
if "%%_arg:~0,1%%"=="-" (
  set "_arg="
  "%%ONIX_EXE%%" %%*
  exit /b
)
set "_arg="

:: Resolve the alias and record the destination in .last. If it isn't known
:: (and isn't an @-segment), fall back to the interactive picker.
"%%ONIX_EXE%%" %%* > "%%ONIX_LAST_FILE%%" 2>nul
if errorlevel 1 (
  echo %%1 | findstr /c:"@" >nul
  if errorlevel 1 call "%%~dp0register.cmd" %%1
)

:: Navigate the current shell to the resolved directory. set /p leaves the
:: variable UNCHANGED on an empty file, so clear it first: an empty .last means
:: the resolve failed or the user cancelled the picker/segment editor, and we
:: must not navigate or open a window — just bail without creating anything.
set "ONIX_LAST="
set /p ONIX_LAST=<"%%ONIX_LAST_FILE%%"
if not defined ONIX_LAST (
  echo [o] nothing to navigate to ^(cancelled, or alias/segment not resolved^) 1>&2
  exit /b 1
)
pushd "%%ONIX_LAST%%" || (
  echo [o] cannot enter "%%ONIX_LAST%%" 1>&2
  exit /b 1
)

:: When launched from Windows Run (Win+R) or by double-click, %%~0 equals the
:: full path %%~f0. In that case open a persistent prompt so the window stays.
if "%%~0"=="%%~f0" cmd /k
`, exe)
	_ = os.WriteFile(path, []byte(content), 0o644)
}

// writeRegisterWrapper emits register.cmd, the unknown-alias fallback the
// navigation shim calls when an alias doesn't resolve. It picks a directory
// with Everything (es) + fzf, cds there, and registers the alias to it.
// Requires the Everything `es` CLI on PATH.
func writeRegisterWrapper(binDir, exe string) {
	path := filepath.Join(binDir, "register.cmd")
	content := fmt.Sprintf(`@echo off
:: onix unknown-alias picker (generated; run 'onix --sync' to regenerate).
:: Pick a directory with Everything (es) + fzf and register the alias to it,
:: writing the resolved path to .last for o.cmd to navigate into. Needs the
:: `+"`es`"+` CLI on PATH. On cancel (empty pick) nothing is registered and we
:: exit non-zero so o.cmd does not navigate or create anything.
set "ONIX_LAST_FILE=%%~dp0\..\.last"
where es >nul 2>&1 || (
  echo [o] Everything 'es' CLI not found on PATH 1>&2
  exit /b 1
)
es %%1 /ad -n 100 | fzf > "%%ONIX_LAST_FILE%%"
set "ONIX_PICK="
set /p ONIX_PICK=<"%%ONIX_LAST_FILE%%"
if not defined ONIX_PICK exit /b 1
"%s" %%1 "%%ONIX_PICK%%" > "%%ONIX_LAST_FILE%%" 2>nul
`, exe)
	_ = os.WriteFile(path, []byte(content), 0o644)
}

// writeExploreWrapper emits the explore shim. With no file argument it
// opens the alias directory by delegating to the run wrapper (resolve the
// dir, cd there, launch `explorer .`) — this avoids explorer.exe's awkward
// cwd handling for the bare-directory case and matches the navigate shims.
// With a file argument it hands off to `onix --explore <file>`, which
// resolves the file to an absolute path and opens it with its default app;
// explorer.exe does not reliably resolve a relative path against the cwd,
// so the run-wrapper trick can't be reused here. runName is the run
// shortcut's wrapper name; exe is the onix binary.
func writeExploreWrapper(binDir, exe, runName, name string) {
	path := filepath.Join(binDir, name+".cmd")
	content := fmt.Sprintf("@echo off\r\n"+
		"if \"%%~2\"==\"\" (\r\n"+
		"  %s %%1 explorer .\r\n"+
		") else (\r\n"+
		"  \"%s\" %%1 --explore \"%%~2\"\r\n"+
		")\r\n", runName, exe)
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
// where `extras` is whatever fixed positionals the flag needs. The
// passthrough walks all remaining args via shift/loop, re-quoting each
// one, so there's no fixed cap and args containing spaces survive.
//
// Each arg is captured into _onix_arg via `set "var=%~1"` first, then
// appended to the accumulator using only delayed expansion. Doing the
// %~1 substitution outside outer quotes (as a one-step `set
// "args=!args! "%~1""`) lets cmd see the substituted special chars
// before redirection scanning — a value like `flag>=` would then be
// parsed as a redirection. The two-step form keeps the literal `>` out
// of the redirection-scanning pass entirely.
func writeAliasFlagWrapper(binDir, exe, name, flag string, extras ...string) {
	path := filepath.Join(binDir, name+".cmd")
	extraStr := ""
	if len(extras) > 0 {
		extraStr = " " + strings.Join(extras, " ")
	}
	content := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion
set "_onix_alias=%%~1"
shift
set "_onix_args="
:_onix_loop
if "%%~1"=="" goto _onix_run
set "_onix_arg=%%~1"
if defined _onix_args (
  set "_onix_args=!_onix_args! "!_onix_arg!""
) else (
  set _onix_args="!_onix_arg!"
)
shift
goto _onix_loop
:_onix_run
"%s" "!_onix_alias!" %s%s !_onix_args!
endlocal
`, exe, flag, extraStr)
	content = strings.ReplaceAll(content, "\n", "\r\n")
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func WriteBashShellSnippet(home string, shortcuts map[string]string) error {
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

	fmt.Fprintf(&b, bashO, s["o"])
	fmt.Fprintf(&b, bashE, s["e"])
	fmt.Fprintf(&b, bashS, s["s"])
	fmt.Fprintf(&b, bashY, s["y"])
	fmt.Fprintf(&b, bashP, s["p"])
	fmt.Fprintf(&b, bashR, s["r"])
	fmt.Fprintf(&b, bashSG, s["sg"])
	fmt.Fprintf(&b, bashFF, s["ff"])
	b.WriteString("\n")

	writeCompleterRegistrationBash(&b, s)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// RegenerateShellSnippet loads the current config and writes a fresh snippet.
func RegenerateShellSnippet(home string) error {
	cfg, err := config.LoadConfig(home)
	if err != nil {
		return err
	}
	return WriteShellSnippet(home, cfg.Shortcuts)
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

func writeCompleterRegistration(b *strings.Builder, shortcuts map[string]string) {
	names := make([]string, 0, len(shortcuts))
	for _, v := range shortcuts {
		names = append(names, v)
	}
	slices.Sort(names)
	fmt.Fprintf(b, "Register-ArgumentCompleter -CommandName %s -ParameterName Alias -ScriptBlock $onixAliasCompleter\n",
		strings.Join(names, ","))
}

func writeCompleterRegistrationBash(b *strings.Builder, shortcuts map[string]string) {
	names := make([]string, 0, len(shortcuts))
	for _, v := range shortcuts {
		names = append(names, v)
	}
	slices.Sort(names)
	fmt.Fprintf(b, `if [ -n "$BASH_VERSION" ]; then
    complete -F _onix_completer %s
elif [ -n "$ZSH_VERSION" ] && command -v compdef >/dev/null 2>&1; then
    compdef _onix_zsh_completer %s
fi
`, strings.Join(names, " "), strings.Join(names, " "))
}
