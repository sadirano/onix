package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/atotto/clipboard"
	"github.com/sadirano/onix/internal/resolver"
	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/snippet"
	"github.com/sadirano/onix/internal/store"
	"github.com/sadirano/onix/internal/usage"
)

// childExitError is returned by RunCmd and ExecCmd when the child process
// exits with a non-zero code. The top-level run() detects this type and
// calls os.Exit with the exact code so deferred cleanup still runs — unlike
// an inline os.Exit which would bypass every defer in the call stack.
type childExitError struct{ Code int }

func (e *childExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// -----------------------------------------------------------------------------
// add — register or update an alias.
//
// We accept either `onix <alias> <path>` or `onix <alias>` (uses CWD; via the
// alias-flag dispatcher, no path means "bare resolve" unless metadata flags
// follow). The path is absolutised here so the store always holds a canonical
// form; users can later move the project and re-add without needing to
// remember what form they originally entered.
// -----------------------------------------------------------------------------

type AddCmd struct {
	Alias string // alias name
	Path  string // directory path (defaults to the current working directory)
}

func (c *AddCmd) Run(ctx context.Context, e *env) error {
	if err := store.ValidateAliasName(c.Alias); err != nil {
		return err
	}
	p := strings.TrimSpace(c.Path)
	if p == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}
		p = cwd
	}
	abs, err := filepath.Abs(store.ExpandTilde(p))
	if err != nil {
		return fmt.Errorf("absolutise %q: %w", p, err)
	}

	// MkdirAll is idempotent and lets `o newalias /new/path` work for
	// directories that don't exist yet — registering the alias and
	// creating the directory are a single intent from the user's view.
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", abs, err)
	}

	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}

	// Merge with existing alias if present.
	alias, _ := s.Lookup(c.Alias)
	alias.Path = filepath.ToSlash(abs)

	s.Set(c.Alias, alias)
	if err := store.SaveStore(e.Home, s); err != nil {
		return err
	}
	// Human-readable confirmation goes to stderr so callers (the `o`
	// shell wrapper, scripts) can capture the resolved path from stdout
	// — same output contract as `onix resolve`.
	fmt.Fprintf(e.Stderr, "registered %s -> %s\n", strings.ToLower(c.Alias), abs)
	fmt.Fprintln(e.Stdout, abs)

	// Registering navigates (the shell wrapper cds to stdout), so it
	// counts as a first use — a fresh alias should not rank as
	// never-used in --prune.
	usage.Record(e.Home, c.Alias)
	return nil
}

// -----------------------------------------------------------------------------
// remove — delete an alias.
// -----------------------------------------------------------------------------

type RemoveCmd struct {
	// Alias names either the alias to remove (when Files is empty) or the
	// directory context for the listed Files. An empty Alias selects ~/.onix.
	Alias string

	// Files lists paths to delete relative to the resolved directory.
	// When non-empty the command acts as a deleter; when empty it removes
	// the alias entry. Use --force to skip the confirm prompt and
	// --recursive to remove directories.
	Files []string

	Force     bool // skip confirmation and bypass guards on load-bearing onix files
	Recursive bool // recursively delete directories
}

// loadBearingOnixFiles lists names that must not be deleted by accident.
// Removing any of these silently breaks onix; we require --force for them
// so the user has to explicitly opt in. Matched case-insensitively against
// the basename of each target path.
var loadBearingOnixFiles = map[string]bool{
	"aliases.toml":  true,
	"config.toml":   true,
	"segments.toml": true,
}

func (c *RemoveCmd) Run(ctx context.Context, e *env) error {
	if len(c.Files) == 0 {
		// No files given: --remove acts on the alias entry itself.
		if c.Alias == "" {
			return fmt.Errorf("--remove requires an alias name or one or more files")
		}
		return c.removeAlias(e)
	}
	return c.deleteFiles(e)
}

// removeAlias deletes the alias entry from aliases.toml (no filesystem
// changes — the user's directory is left untouched).
func (c *RemoveCmd) removeAlias(e *env) error {
	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}
	if !s.Delete(c.Alias) {
		return fmt.Errorf("unknown alias %q", c.Alias)
	}
	if err := store.SaveStore(e.Home, s); err != nil {
		return err
	}
	usage.Remove(e.Home, []string{c.Alias})
	fmt.Fprintf(e.Stderr, "removed %s\n", strings.ToLower(c.Alias))
	return nil
}

// deleteFiles deletes each path in Files relative to the resolved base
// directory (alias dir if Alias is set, else ~/.onix). Confirms once for
// the whole batch unless --force is set.
func (c *RemoveCmd) deleteFiles(e *env) error {
	base := e.Home
	if c.Alias != "" {
		d, err := resolveAliasPath(e, c.Alias)
		if err != nil {
			return err
		}
		base = d
	}

	// Resolve each target and run pre-delete safety checks before we
	// touch anything. Bailing on the first problem keeps the operation
	// atomic-feeling — the user doesn't see "deleted 2 of 5" with the
	// other 3 silently rejected.
	type target struct {
		display string
		abs     string
		info    os.FileInfo
	}
	targets := make([]target, 0, len(c.Files))
	for _, f := range c.Files {
		abs := f
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(base, f)
		}
		info, err := os.Lstat(abs)
		if err != nil {
			return fmt.Errorf("delete %s: %w", f, err)
		}
		if info.IsDir() && !c.Recursive {
			return fmt.Errorf("delete %s: is a directory (pass --recursive to remove)", f)
		}
		if !c.Force && c.Alias == "" && loadBearingOnixFiles[strings.ToLower(info.Name())] {
			return fmt.Errorf("delete %s: refusing to delete load-bearing onix file without --force", f)
		}
		targets = append(targets, target{display: f, abs: abs, info: info})
	}

	if !c.Force {
		fmt.Fprintf(e.Stderr, "Delete %d item(s) from %s? [y/N] ", len(targets), base)
		var resp string
		_, _ = fmt.Fscanln(e.Stdin, &resp)
		resp = strings.TrimSpace(strings.ToLower(resp))
		if resp != "y" && resp != "yes" {
			return fmt.Errorf("aborted")
		}
	}

	for _, t := range targets {
		var rmErr error
		if t.info.IsDir() {
			rmErr = os.RemoveAll(t.abs)
		} else {
			rmErr = os.Remove(t.abs)
		}
		if rmErr != nil {
			return fmt.Errorf("delete %s: %w", t.display, rmErr)
		}
		fmt.Fprintf(e.Stderr, "deleted %s\n", t.display)
	}
	return nil
}

// -----------------------------------------------------------------------------
// list — print aliases in a stable, scannable table.
//
// We use tabwriter rather than fixed-width fmt so long names/paths align
// without truncation. --json switches to a machine-readable list for
// scripting.
// -----------------------------------------------------------------------------

type ListCmd struct{}

func (c *ListCmd) Run(ctx context.Context, e *env) error {
	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}
	names := s.Names()

	if e.JSON {
		type aliasInfo struct {
			Name string `json:"name"`
			Path string `json:"path,omitempty"`
		}
		out := make([]aliasInfo, 0, len(names))
		for _, n := range names {
			a, _ := s.Lookup(n)
			out = append(out, aliasInfo{Name: n, Path: a.Path})
		}
		return printJSON(e.Stdout, out)
	}

	if len(names) == 0 {
		fmt.Fprintln(e.Stdout, "no aliases registered (run: onix <name> <path>)")
		return nil
	}
	w := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ALIAS\tPATH")
	for _, n := range names {
		a, _ := s.Lookup(n)
		if a.Path == "" {
			fmt.Fprintf(w, "%s\t(no path)\n", n)
		} else {
			fmt.Fprintf(w, "%s\t%s\n", n, a.Path)
		}
	}
	return w.Flush()
}

// -----------------------------------------------------------------------------
// prune — interactively remove stale aliases.
//
// Candidates are every registered alias, ranked prime-victims-first: dead
// targets (directory gone), then never-used, then least-recently used,
// based on the usage file the resolve paths maintain. The fzf multi-select
// IS the confirmation: Enter removes the marked rows, Esc removes nothing.
// With --no-prompt the ranking is printed and nothing is deleted.
// -----------------------------------------------------------------------------

type PruneCmd struct{}

func (c *PruneCmd) Run(ctx context.Context, e *env) error {
	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}
	if len(s.Aliases) == 0 {
		fmt.Fprintln(e.Stdout, "no aliases registered (run: onix <name> <path>)")
		return nil
	}
	u := usage.Load(e.Home)

	type candidate struct {
		name, path string
		ent        usage.Entry
		dead       bool
	}
	cands := make([]candidate, 0, len(s.Aliases))
	nameWidth := 0
	for _, name := range s.Names() {
		a, _ := s.Lookup(name)
		_, statErr := os.Stat(store.ExpandTilde(a.Path))
		cands = append(cands, candidate{name: name, path: a.Path, ent: u[name], dead: statErr != nil})
		nameWidth = max(nameWidth, len(name))
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].dead != cands[j].dead {
			return cands[i].dead
		}
		return cands[i].ent.Last < cands[j].ent.Last // never-used (0) sorts first
	})

	nowUnix := time.Now().Unix()
	var b strings.Builder
	for _, cd := range cands {
		marker := ""
		if cd.dead {
			marker = "  [gone]"
		}
		fmt.Fprintf(&b, "%-*s  %9s  %4d uses  %s%s\n",
			nameWidth, cd.name, humanAge(cd.ent.Last, nowUnix), cd.ent.Count, cd.path, marker)
	}

	if e.NoPrompt {
		fmt.Fprint(e.Stdout, b.String())
		return nil
	}
	if _, err := lookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found on PATH (use --no-prompt to just print the ranking)")
	}

	fzfCmd := execCommandContext(ctx, "fzf", "--multi", "--layout=reverse",
		"--header", "prune: Tab marks, Enter removes marked aliases, Esc cancels")
	fzfCmd.Stdin = strings.NewReader(b.String())
	fzfCmd.Stderr = os.Stderr // fzf UI uses stderr when stdout is captured
	applyDefaultFzfTheme(fzfCmd)

	selected, err := fzfCmd.Output()
	if err != nil {
		// Same contract as the other fzf consumers: 130 is Esc, 1 is
		// nothing-matched — both mean "remove nothing".
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}

	var removed []string
	for _, line := range strings.Split(strings.TrimSpace(string(selected)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if s.Delete(fields[0]) {
			removed = append(removed, fields[0])
		}
	}
	if len(removed) == 0 {
		fmt.Fprintln(e.Stderr, "nothing pruned")
		return nil
	}
	if err := store.SaveStore(e.Home, s); err != nil {
		return err
	}
	usage.Remove(e.Home, removed)
	fmt.Fprintf(e.Stderr, "pruned %d: %s\n", len(removed), strings.Join(removed, ", "))
	return nil
}

// humanAge renders a last-used unix stamp for the prune listing.
func humanAge(last, now int64) string {
	if last == 0 {
		return "never"
	}
	days := (now - last) / 86400
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1d ago"
	default:
		return fmt.Sprintf("%dd ago", days)
	}
}

// -----------------------------------------------------------------------------
// edit — open the alias directory in the user's editor.
//
// Editor precedence: $EDITOR > "nvim". We pass "." as the path so the editor
// opens the directory as a project, not as a file. We inherit stdin/stdout/
// stderr because terminal editors (nvim, vim) need a real tty.
// -----------------------------------------------------------------------------

type EditCmd struct {
	// Alias selects the directory the editor opens in. An empty Alias is the
	// system-wide form and opens ~/.onix.
	Alias string

	// Files lists paths relative to the resolved directory. When empty the
	// editor opens the directory itself ("."), matching how most editors
	// treat a project. With files we pass them verbatim so editor-specific
	// `+line` syntax keeps working when callers prepend it.
	Files []string
}

func (c *EditCmd) Run(ctx context.Context, e *env) error {
	dir, err := c.targetDir(e)
	if err != nil {
		return err
	}
	ed := resolveEditor()
	if ed == "" {
		return fmt.Errorf("no $EDITOR set and none of the standard editors (nvim, vim, code, nano, notepad) found on PATH")
	}
	args := c.Files
	if len(args) == 0 {
		args = []string{"."}
	}
	cmd := execCommandContext(ctx, ed, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = e.Stdout
	cmd.Stderr = e.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %s: %w", ed, err)
	}
	return nil
}

// targetDir resolves where the editor should run. Empty alias = the onix
// config home; otherwise the alias's directory.
func (c *EditCmd) targetDir(e *env) (string, error) {
	if c.Alias == "" {
		return e.Home, nil
	}
	return resolveAliasPath(e, c.Alias)
}

// -----------------------------------------------------------------------------
// explore — open the OS file manager at the alias directory, or open a
// specific file with its default application.
//
// With no file, opens the directory in the file manager. With a file, hands
// it to the same OS opener (explorer.exe / xdg-open), which launches the
// file's associated app — a PDF in the PDF viewer, a .zip in the archive
// tool, and so on. The file is resolved to an absolute path first because
// explorer.exe does not reliably resolve a relative path against the cwd.
//
// Windows uses explorer.exe directly (no cmd.exe wrapper, no /e flag — both
// add startup overhead or hide bugs). Linux uses xdg-open. macOS is not
// supported.
// -----------------------------------------------------------------------------

type ExploreCmd struct {
	Alias string // alias name
	File  string // file to open with its default app (omit to open the directory)
}

func (c *ExploreCmd) Run(ctx context.Context, e *env) error {
	dir, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
	}
	target := dir
	if c.File != "" {
		target = c.File
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("open %q: %w", c.File, err)
		}
	}
	return openInExplorer(target)
}

// -----------------------------------------------------------------------------
// yank — print path + copy to clipboard.
//
// Two side effects on stdout: the path itself (so it composes in pipes) and
// the clipboard copy (so the user can paste it elsewhere). We don't persist
// to ONIX_LAST anymore — setx was Windows-only, slow, and never worked for
// the current shell anyway. The shell function story is the replacement.
// -----------------------------------------------------------------------------

type YankCmd struct {
	Alias string `arg:"" help:"Alias name."`
}

func (c *YankCmd) Run(ctx context.Context, e *env) error {
	target, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, target)
	if err := copyToClipboard(target); err != nil {
		// Non-fatal — we already printed the path so the user can copy it
		// manually. Warn so they know the clipboard step didn't work.
		fmt.Fprintf(e.Stderr, "warning: clipboard copy failed: %v\n", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// paste — save clipboard content into an alias dir, then copy the saved
// file's path back to the clipboard.
//
// The round-trip exists so a screenshot (or copied text) can be parked in a
// known location and the resulting path handed straight to an AI agent. The
// path overwrites the clipboard image, which is non-destructive on Windows
// because the image is still recoverable from clipboard history (Win+V).
//
// Content type drives the default extension: an image saves as .png, text as
// .md. An explicit extension on the name is always honoured. With no name we
// fall back to a timestamp. Collisions auto-increment (cool.png, cool-1.png).
// -----------------------------------------------------------------------------

type PasteCmd struct {
	Alias string
	Name  string
}

func (c *PasteCmd) Run(ctx context.Context, e *env) error {
	target, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
	}
	data, defaultExt, err := readClipboardContent()
	if err != nil {
		return err
	}
	dest := uniquePath(filepath.Join(target, pasteFilename(c.Name, defaultExt)))
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", dest, err)
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		abs = dest
	}
	out := filepath.ToSlash(abs)
	fmt.Fprintln(e.Stdout, out)
	if err := copyToClipboard(out); err != nil {
		// Non-fatal — the file is saved and the path is on stdout, so the
		// user can still copy it manually.
		fmt.Fprintf(e.Stderr, "warning: clipboard copy failed: %v\n", err)
	}
	return nil
}

// readClipboardContent is implemented per-platform: clipboard_windows.go
// reads images/text via golang.design/x/clipboard; the non-Windows build
// returns an error because that library needs cgo + X11 on Linux.

// pasteFilename builds the destination filename. An explicit extension on the
// name is honoured; otherwise defaultExt (from the clipboard content type) is
// appended. An empty name falls back to a timestamp.
func pasteFilename(name, defaultExt string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		// Colons are illegal in Windows filenames, so no time-of-day colons.
		return time.Now().Format("2006-01-02_150405") + defaultExt
	}
	if filepath.Ext(name) != "" {
		return name
	}
	return name + defaultExt
}

// uniquePath returns path unchanged if nothing is there, otherwise the first
// free "<stem>-<n><ext>" variant so a repeated paste never clobbers an
// earlier file.
func uniquePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

// -----------------------------------------------------------------------------
// run — execute a command in the alias directory.
//
// `onix acme --run go test ./...`
//
// Everything after --run is captured verbatim as argv. We do NOT invoke a
// shell here — extras are exec'd as argv directly so the user's quoting
// reaches the child process without a re-parse.
// -----------------------------------------------------------------------------

// RunCmd uses a single positional slice (rather than separate Alias+Cmd
// fields) so the dispatcher can hand the whole "<alias> <cmd> [args...]" tail
// through untouched; we split the alias off ourselves below.
type RunCmd struct {
	Args []string // <alias> <cmd> [args...]
}

func (c *RunCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: onix <alias> --run <cmd> [args...]")
	}
	alias := c.Args[0]
	target, err := resolveAliasPath(e, alias)
	if err != nil {
		return err
	}
	// Kong's passthrough mode usually swallows the "--" separator, but the
	// behaviour shifts between releases and the PowerShell shell function
	// inserts one unconditionally. Strip it defensively so users get the
	// same argv regardless of how they invoked us.
	argv := c.Args[1:]
	outside := false
	if len(argv) > 0 && (argv[0] == "-o" || argv[0] == "--outside") {
		outside = true
		argv = argv[1:]
	}
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return fmt.Errorf("usage: onix <alias> --run <cmd> [args...]")
	}
	exe := argv[0]
	// On Windows, Go's exec.LookPath refuses to run executables found relative
	// to the current directory (security policy added in Go 1.19). Since we
	// want bare names like "run" to resolve inside the alias directory, probe
	// target explicitly with the standard Windows executable extensions before
	// falling back to PATH lookup.
	if runtime.GOOS == "windows" && filepath.Base(exe) == exe {
		for _, ext := range []string{".cmd", ".bat", ".exe", ".ps1"} {
			candidate := filepath.Join(target, exe+ext)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				exe = candidate
				break
			}
		}
	}
	if outside {
		return runCommandOutside(target, exe, argv[1:])
	}
	cmd := execCommandContext(ctx, exe, argv[1:]...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = e.Stdout
	cmd.Stderr = e.Stderr
	if err := cmd.Run(); err != nil {
		// Propagate child exit codes verbatim. Without this a `go test`
		// failure inside `onix run` would surface as a generic exit 1.
		// We return a childExitError so the deferred panic-recovery in
		// run() still executes before os.Exit is called.
		if ee, ok := err.(*exec.ExitError); ok {
			return &childExitError{Code: ee.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// sync — regenerate shell snippets and Windows wrappers.
//
// Separate from `init` because users will edit config.toml multiple times
// over a session and we don't want to keep re-touching $PROFILE on each
// edit. sync rewrites ~/.onix/shell/onix.ps1 and ~/.onix/bin/*.cmd; the
// dot-source line in $PROFILE already points at the snippet.
// -----------------------------------------------------------------------------

type SyncCmd struct{}

func (c *SyncCmd) Run(ctx context.Context, e *env) error {
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintf(e.Stderr, "regenerated %s and wrappers in %s\n", snippet.PwshPath(e.Home), filepath.Join(e.Home, "bin"))
		if p, ok, err := snippet.RefreshClinkLua(e.Home); err != nil {
			fmt.Fprintf(e.Stderr, "warning: clink integration: %v\n", err)
		} else if ok {
			fmt.Fprintf(e.Stderr, "refreshed clink integration: %s\n", p)
		}
	} else {
		fmt.Fprintf(e.Stderr, "regenerated %s\n", snippet.BashPath(e.Home))
	}
	fmt.Fprintln(e.Stderr, "re-source $PROFILE (or restart PowerShell) to pick up changes")
	return nil
}

// -----------------------------------------------------------------------------
// shared helpers
// -----------------------------------------------------------------------------

func resolveAliasPath(e *env, name string) (string, error) {
	var segPrompter resolver.SegmentPrompter

	if !e.NoPrompt {
		reader := bufio.NewReader(e.Stdin)
		segPrompter = func(segmentName, inlineValue, aliasBase, aliasName string) (*segments.ContextDef, error) {
			return promptSegmentDefinition(e.Home, segmentName, inlineValue, e.Stderr, reader, aliasBase, aliasName)
		}
	}

	p, err := resolver.Resolve(e.Home, name, segPrompter, e.Timer)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", fmt.Errorf("create directory %q: %w", p, err)
	}

	// Frecency bookkeeping: every alias action (edit/run/grep/...) counts
	// as a use of the base alias, so --prune ranks by real activity rather
	// than just bare-resolve navigation.
	_, base := segments.ParseSegmentedAlias(name)
	usage.Record(e.Home, base)
	return p, nil
}

// resolveEditor returns the editor to invoke for `onix edit`.
// Lookup order: $EDITOR, $VISUAL, then common binaries.
func resolveEditor() string {
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("VISUAL")); e != "" {
		return e
	}
	for _, e := range []string{"nvim", "vim", "code", "nano", "notepad"} {
		if _, err := lookPath(e); err == nil {
			return e
		}
	}
	return ""
}

// copyToClipboard writes s to the system clipboard. We use atotto/clipboard
// so it works natively on Windows and Linux (via xclip/xsel).
func copyToClipboard(s string) error {
	return clipboard.WriteAll(s)
}
