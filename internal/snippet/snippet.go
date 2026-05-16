package snippet

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/plugins"
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
        & $global:onixExe aliases
        return
    }

    # Passthrough to onix for subcommands.
    if ($Alias -in @('add','rm','remove','ls','list','aliases','edit','grep','find','explore','yank','run','exec','plugin','import','context','init','sync','doctor','version')) {
        & $global:onixExe $Alias @args
        return
    }

    if ($Path) {
        # '%s foo C:\some\path' — register (or update) the alias and cd
        # into it. The directory is auto-created by 'onix add' if it
        # doesn't exist.
        $resolved = & $global:onixExe add $Alias $Path
    } else {
        $resolved = & $global:onixExe resolve $Alias
    }
    if ($LASTEXITCODE -eq 0) {
        Set-Location -LiteralPath $resolved
        & $global:onixExe context apply $Alias | Invoke-Expression
    }
}
`

const pwshN = `function global:%s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe edit $Alias
}
`

const pwshS = `function global:%s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe explore $Alias
}
`

const pwshY = `function global:%s {
    [CmdletBinding()]
    param([Parameter(Position=0, Mandatory=$true)][string]$Alias)
    & $global:onixExe yank $Alias
}
`

const pwshR = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, Mandatory=$true, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe run $Alias -- @Rest
}
`

const pwshSG = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe grep $Alias @Rest
}
`

const pwshFF = `function global:%s {
    [CmdletBinding()]
    param(
        [Parameter(Position=0, Mandatory=$true)][string]$Alias,
        [Parameter(Position=1, ValueFromRemainingArguments=$true)][string[]]$Rest
    )
    & $global:onixExe find $Alias @Rest
}
`

const bashO = `%s() {
    if [ -z "$1" ]; then
        "$ONIX_EXE" aliases
        return
    fi
    case "$1" in
        add|rm|remove|ls|list|aliases|edit|grep|find|explore|yank|run|exec|plugin|import|context|init|sync|doctor|version)
            "$ONIX_EXE" "$@"
            return
            ;;
    esac
    local path
    if [ -n "$2" ]; then
        # '%s foo /some/path' — register (or update) the alias and cd into
        # it. The directory is auto-created by 'onix add' if missing.
        path=$("$ONIX_EXE" add "$1" "$2")
    else
        path=$("$ONIX_EXE" resolve "$1")
    fi
    if [ $? -eq 0 ]; then
        cd "$path"
        local shell="bash"
        if [ -n "$ZSH_VERSION" ]; then shell="zsh"; fi
        eval "$("$ONIX_EXE" context apply "$1" --shell "$shell")"
    fi
}
`

const bashN = `%s() { "$ONIX_EXE" edit "$1"; }
`

const bashS = `%s() { "$ONIX_EXE" explore "$1"; }
`

const bashY = `%s() { "$ONIX_EXE" yank "$1"; }
`

const bashR = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" run "$alias" -- "$@"
}
`

const bashSG = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" grep "$alias" "$@"
}
`

const bashFF = `%s() {
    local alias=$1
    shift
    "$ONIX_EXE" find "$alias" "$@"
}
`

const pwshCompleter = `$onixAliasCompleter = {
    param($wordToComplete, $commandAst, $cursorPosition)
    @(& $global:onixExe list-names 2>$null) |
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

// WriteShellSnippet regenerates the host-platform shell snippet.
func WriteShellSnippet(home string, shortcuts map[string]string, actions []config.Action, plgs []plugins.Plugin) error {
	if runtime.GOOS == "windows" {
		return WritePwshShellSnippet(home, shortcuts, actions, plgs)
	}
	return WriteBashShellSnippet(home, shortcuts, actions, plgs)
}

func WritePwshShellSnippet(home string, shortcuts map[string]string, actions []config.Action, plgs []plugins.Plugin) error {
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
	fmt.Fprintf(&b, pwshN, s["n"])
	fmt.Fprintf(&b, pwshS, s["s"])
	fmt.Fprintf(&b, pwshY, s["y"])
	fmt.Fprintf(&b, pwshR, s["r"])
	fmt.Fprintf(&b, pwshSG, s["sg"])
	fmt.Fprintf(&b, pwshFF, s["ff"])
	b.WriteString("\n")

	// On Windows, we also drop .cmd wrappers into ~/.onix/bin for each
	// shortcut, custom action, and plugin entry. This makes them
	// available via Windows Run (Win+R) or from cmd.exe without needing
	// the PowerShell snippet.
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)

	writeOCmdWrapper(binDir, exe, s["o"])
	writeCmdWrapper(binDir, exe, s["n"], "edit")
	writeCmdWrapper(binDir, exe, s["s"], "explore")
	writeCmdWrapper(binDir, exe, s["y"], "yank")
	writeCmdWrapper(binDir, exe, s["r"], "run")
	writeCmdWrapper(binDir, exe, s["sg"], "grep")
	writeCmdWrapper(binDir, exe, s["ff"], "find")

	for _, a := range actions {
		writeActionFunction(&b, a)
		writeCmdWrapper(binDir, exe, a.Name, "exec", a.Name)
	}

	for _, p := range plgs {
		writePluginFunction(&b, p.Name, p.Name, "")
		writeCmdWrapper(binDir, exe, p.Name, "plugin-exec", p.Name, "")

		for _, e := range p.Entries {
			writePluginFunction(&b, e.EffectiveCmd(), p.Name, e.Name)
			writeCmdWrapper(binDir, exe, e.EffectiveCmd(), "plugin-exec", p.Name, e.Name)
		}
	}

	writeCompleterRegistration(&b, s, actions, plgs)

	return os.WriteFile(path, []byte(b.String()), 0o644)
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
  "%s" aliases
  exit /b
)

set "_onix_target="
for /f "usebackq delims=" %%%%i in (`+"`"+`"%s" resolve --no-prompt %%~1 2^>nul`+"`"+`) do set "_onix_target=%%%%i"
if not defined _onix_target (
  "%s" %%*
  exit /b
)

cd /d "%%_onix_target%%"
set "_onix_target="
for /f "usebackq delims=" %%%%i in (`+"`"+`"%s" context apply %%~1 --shell cmd`+"`"+`) do %%%%i
if %%0 == "%%~f0" cmd /k
`, exe, exe, exe, exe)
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func writeCmdWrapper(binDir, exe, name string, args ...string) {
	path := filepath.Join(binDir, name+".cmd")
	var cmdArgs []string
	for _, a := range args {
		if a == "" {
			cmdArgs = append(cmdArgs, `""`)
		} else {
			cmdArgs = append(cmdArgs, a)
		}
	}
	content := fmt.Sprintf("@echo off\r\n\"%s\" %s %%*\r\n", exe, strings.Join(cmdArgs, " "))
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func WriteBashShellSnippet(home string, shortcuts map[string]string, actions []config.Action, plgs []plugins.Plugin) error {
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
	fmt.Fprintf(&b, bashN, s["n"])
	fmt.Fprintf(&b, bashS, s["s"])
	fmt.Fprintf(&b, bashY, s["y"])
	fmt.Fprintf(&b, bashR, s["r"])
	fmt.Fprintf(&b, bashSG, s["sg"])
	fmt.Fprintf(&b, bashFF, s["ff"])
	b.WriteString("\n")

	for _, a := range actions {
		writeActionFunctionBash(&b, a)
	}

	for _, p := range plgs {
		writePluginFunctionBash(&b, p.Name, p.Name, "")
		for _, e := range p.Entries {
			writePluginFunctionBash(&b, e.EffectiveCmd(), p.Name, e.Name)
		}
	}

	writeCompleterRegistrationBash(&b, s, actions, plgs)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// RegenerateShellSnippet loads the current config + plugin state and writes a fresh snippet.
func RegenerateShellSnippet(home string) error {
	cfg, err := config.LoadConfig(home)
	if err != nil {
		return err
	}
	pf, err := plugins.LoadPlugins(home)
	if err != nil {
		return err
	}
	return WriteShellSnippet(home, cfg.Shortcuts, cfg.Actions, pf.Plugins)
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
    & $global:onixExe exec %s $Alias -- @Rest
}

`, a.Name, a.Name)
}

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

func writeCompleterRegistration(b *strings.Builder, shortcuts map[string]string, actions []config.Action, plgs []plugins.Plugin) {
	names := make([]string, 0, 7+len(actions)+len(plgs))
	for _, v := range shortcuts {
		names = append(names, v)
	}
	for _, a := range actions {
		names = append(names, a.Name)
	}
	for _, p := range plgs {
		names = append(names, p.Name)
		for _, e := range p.Entries {
			names = append(names, e.EffectiveCmd())
		}
	}
	slices.Sort(names)
	fmt.Fprintf(b, "Register-ArgumentCompleter -CommandName %s -ParameterName Alias -ScriptBlock $onixAliasCompleter\n",
		strings.Join(names, ","))
}

func writeActionFunctionBash(b *strings.Builder, a config.Action) {
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

func writeCompleterRegistrationBash(b *strings.Builder, shortcuts map[string]string, actions []config.Action, plgs []plugins.Plugin) {
	names := make([]string, 0, 7+len(actions)+len(plgs))
	for _, v := range shortcuts {
		names = append(names, v)
	}
	for _, a := range actions {
		names = append(names, a.Name)
	}
	for _, p := range plgs {
		names = append(names, p.Name)
		for _, e := range p.Entries {
			names = append(names, e.EffectiveCmd())
		}
	}
	slices.Sort(names)
	fmt.Fprintf(b, `if [ -n "$BASH_VERSION" ]; then
    complete -F _onix_completer %s
elif [ -n "$ZSH_VERSION" ] && command -v compdef >/dev/null 2>&1; then
    compdef _onix_zsh_completer %s
fi
`, strings.Join(names, " "), strings.Join(names, " "))
}
