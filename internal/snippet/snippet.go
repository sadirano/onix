package snippet

import (
	"fmt"
	"io"
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

// On Windows, onix ships as a multi-call binary: the same executable is
// hardlinked into ~/.onix/bin under each command name (o, e, r, ...), and the
// binary recovers the action from argv[0]. The PowerShell snippet therefore no
// longer defines o/e/... functions — it only puts the wrapper dir on PATH for
// the current session and registers alias tab-completion. POSIX keeps the
// shell-function model below (cd-in-place, no files, no lock to fix).

// bashO and friends define the POSIX shell functions. These stay as functions
// because a function can cd the calling shell in place — the cleaner UX on
// POSIX.
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

// pwshCompleter registers a NATIVE argument completer (the wrappers are real
// executables now, not functions, so -ParameterName no longer applies). It
// only suggests alias names for the first argument.
const pwshCompleter = `$onixAliasCompleter = {
    param($wordToComplete, $commandAst, $cursorPosition)
    # Only complete the first argument (the alias); leave a command's later
    # arguments to the shell's own file/command completion.
    if ($commandAst.CommandElements.Count -gt 2) { return }
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

// WriteShellSnippet regenerates the host-platform shell snippet (and, on
// Windows, installs the multi-call .exe wrappers).
func WriteShellSnippet(home string, shortcuts map[string]string) error {
	if runtime.GOOS == "windows" {
		return WritePwshShellSnippet(home, shortcuts)
	}
	return WriteBashShellSnippet(home, shortcuts)
}

// WritePwshShellSnippet writes the PowerShell integration (completer + session
// PATH) and installs the multi-call wrappers into ~/.onix/bin. The wrappers
// are what actually run o/e/r/...; the snippet only registers tab-completion
// and front-loads the wrapper dir onto PATH so a freshly sourced session finds
// them before the persistent user-PATH entry reaches new shells.
func WritePwshShellSnippet(home string, shortcuts map[string]string) error {
	path := PwshPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	exe, err := resolveOnixExe()
	if err != nil {
		return err
	}

	s := config.BuiltinDefaults()
	for k, v := range shortcuts {
		if _, ok := s[k]; ok {
			s[k] = v
		}
	}

	binDir := filepath.Join(home, "bin")

	var b strings.Builder
	b.WriteString("# onix shell integration (generated; do not edit — run 'onix sync')\n")
	fmt.Fprintf(&b, "# Source from $PROFILE: . '%s'\n\n", strings.ReplaceAll(path, `'`, `''`))
	fmt.Fprintf(&b, "$global:onixExe = '%s'\n\n", strings.ReplaceAll(exe, `'`, `''`))

	// Prepend the wrapper dir to PATH for this session immediately; the
	// persistent user-PATH entry (added by `onix --init`) only reaches shells
	// started afterwards.
	fmt.Fprintf(&b, "$onixBin = '%s'\n", strings.ReplaceAll(binDir, `'`, `''`))
	b.WriteString("if (($env:PATH -split ';') -notcontains $onixBin) { $env:PATH = $onixBin + ';' + $env:PATH }\n\n")

	b.WriteString(pwshCompleter)
	b.WriteString("\n")
	b.WriteString(pwshQ)
	b.WriteString("\n")

	writeCompleterRegistration(&b, s)

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}

	// Install the multi-call wrappers (o.exe, e.exe, ...) last so a snippet is
	// still produced even if linking hits trouble.
	return installExeWrappers(binDir, exe, s)
}

// installExeWrappers makes the onix binary available under each command name
// in binDir. The canonical binary is kept at binDir/onix.exe (copied in when
// onix is being run from elsewhere, e.g. a build directory) so every per-name
// wrapper can hardlink to it on the same volume; linking falls back to a copy
// across volumes or on filesystems without hardlink support. Existing wrappers
// are refreshed in place, so a `sync` after an upgrade re-points them at the
// current binary.
func installExeWrappers(binDir, exe string, shortcuts map[string]string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", binDir, err)
	}

	canonical := filepath.Join(binDir, "onix"+exeExt())
	if !samePath(canonical, exe) {
		if err := copyFile(exe, canonical); err != nil {
			return fmt.Errorf("install onix binary into %s: %w", binDir, err)
		}
	}

	for _, name := range shortcuts {
		dst := filepath.Join(binDir, name+exeExt())
		if samePath(dst, canonical) {
			continue
		}
		// Best-effort: a wrapper that's currently running can't be replaced on
		// Windows (it shares the binary's image). Leave the old one in place and
		// move on; the next sync, once it's no longer running, refreshes it.
		_ = linkOrCopy(canonical, dst)
	}

	return nil
}

// exeExt is the executable suffix for the host platform.
func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// linkOrCopy refreshes dst as a hardlink to src, falling back to a copy when
// hardlinking isn't possible (cross-volume, unsupported filesystem).
func linkOrCopy(src, dst string) error {
	_ = os.Remove(dst) // drop any stale link/copy so the link can be recreated
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

// copyFile copies src to dst atomically (write-temp-then-rename) with an
// executable mode.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// samePath reports whether two paths point at the same location, comparing
// case-insensitively on Windows.
func samePath(a, b string) bool {
	ca, err := filepath.Abs(a)
	if err != nil {
		ca = a
	}
	cb, err := filepath.Abs(b)
	if err != nil {
		cb = b
	}
	ca, cb = filepath.Clean(ca), filepath.Clean(cb)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
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
	fmt.Fprintf(b, "Register-ArgumentCompleter -Native -CommandName %s -ScriptBlock $onixAliasCompleter\n",
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
