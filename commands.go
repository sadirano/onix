package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/atotto/clipboard"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/plugins"
	"github.com/sadirano/onix/internal/resolver"
	"github.com/sadirano/onix/internal/segments"
	"github.com/sadirano/onix/internal/snippet"
	"github.com/sadirano/onix/internal/store"
)

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
	Alias       string   `arg:"" help:"Alias name."`
	Path        string   `arg:"" optional:"" help:"Directory path (default: current working directory)."`
	Description string   `help:"Human-readable description of the alias."`
	Owner       string   `help:"The person or team responsible for this directory."`
	Tags        []string `help:"Categorization labels (multiple flags)."`
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
	if c.Description != "" {
		alias.Description = c.Description
	}
	if c.Owner != "" {
		alias.Owner = c.Owner
	}
	if len(c.Tags) > 0 {
		alias.Tags = c.Tags
	}

	s.Set(c.Alias, alias)
	if err := store.SaveStore(e.Home, s); err != nil {
		return err
	}
	// Human-readable confirmation goes to stderr so callers (the `o`
	// shell wrapper, scripts) can capture the resolved path from stdout
	// — same output contract as `onix resolve`.
	fmt.Fprintf(e.Stderr, "registered %s -> %s\n", strings.ToLower(c.Alias), abs)
	fmt.Fprintln(e.Stdout, abs)
	return nil
}

// -----------------------------------------------------------------------------
// remove — delete an alias.
// -----------------------------------------------------------------------------

type RemoveCmd struct {
	// Alias names the alias to remove (legacy form) or the directory
	// context for Files (new form). Empty selects ~/.onix.
	Alias string `arg:"" optional:"" help:"Alias name."`

	// Files lists paths to delete relative to the resolved directory.
	// When non-empty the command acts as a deleter; when empty it removes
	// the alias entry. Use --force to skip the confirm prompt and
	// --recursive to remove directories.
	Files []string `arg:"" optional:"" passthrough:"" help:"Files to delete (relative to the resolved directory)."`

	Force     bool `name:"force" short:"F" help:"Skip confirmation and bypass guards on load-bearing onix files."`
	Recursive bool `name:"recursive" short:"R" help:"Recursively delete directories."`
}

// loadBearingOnixFiles lists names that must not be deleted by accident.
// Removing any of these silently breaks onix; we require --force for them
// so the user has to explicitly opt in. Matched case-insensitively against
// the basename of each target path.
var loadBearingOnixFiles = map[string]bool{
	"aliases.toml":  true,
	"config.toml":   true,
	"segments.toml": true,
	"plugins.toml":  true,
	"usage.log":     true,
}

func (c *RemoveCmd) Run(ctx context.Context, e *env) error {
	if len(c.Files) == 0 {
		// Legacy form: remove the alias.
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
// without truncation. JSON output comes later when we wire scripting helpers.
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
			Name        string   `json:"name"`
			Path        string   `json:"path"`
			Description string   `json:"description,omitempty"`
			Tags        []string `json:"tags,omitempty"`
			Owner       string   `json:"owner,omitempty"`
			LastUsed    int64    `json:"last_used,omitempty"`
		}
		out := make([]aliasInfo, 0, len(names))
		for _, n := range names {
			a, _ := s.Lookup(n)
			out = append(out, aliasInfo{
				Name:        n,
				Path:        a.Path,
				Description: a.Description,
				Tags:        a.Tags,
				Owner:       a.Owner,
				LastUsed:    a.LastUsed,
			})
		}
		return printJSON(e.Stdout, out)
	}

	if len(names) == 0 {
		fmt.Fprintln(e.Stdout, "no aliases registered (run: onix <name> <path>)")
		return nil
	}
	w := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ALIAS\tPATH\tDESCRIPTION")
	for _, n := range names {
		a, _ := s.Lookup(n)
		fmt.Fprintf(w, "%s\t%s\t%s\n", n, a.Path, a.Description)
	}
	return w.Flush()
}

// -----------------------------------------------------------------------------
// edit — open the alias directory in the user's editor.
//
// Editor precedence: $EDITOR > "nvim". We pass "." as the path so the editor
// opens the directory as a project, not as a file. We inherit stdin/stdout/
// stderr because terminal editors (nvim, vim) need a real tty.
// -----------------------------------------------------------------------------

type EditCmd struct {
	// Alias selects the directory the editor opens in. Empty means the
	// system-wide form: open ~/.onix (the new dispatcher uses this; the
	// legacy `onix edit <alias>` subcommand requires it).
	Alias string `arg:"" optional:"" help:"Alias name (omit for the onix config directory)."`

	// Files lists paths relative to the resolved directory. When empty the
	// editor opens the directory itself ("."), matching how most editors
	// treat a project. With files we pass them verbatim so editor-specific
	// `+line` syntax keeps working when callers prepend it.
	Files []string `arg:"" optional:"" passthrough:"" help:"Files (relative to the resolved directory)."`
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
// explore — open the OS file manager at the alias directory.
//
// Windows uses explorer.exe directly (no cmd.exe wrapper, no /e flag — both
// add startup overhead or hide bugs). Unix builds get this in M3; we error
// loudly until then so a user on Linux knows we haven't shipped it.
// -----------------------------------------------------------------------------

type ExploreCmd struct {
	Alias string `arg:"" help:"Alias name."`
}

func (c *ExploreCmd) Run(ctx context.Context, e *env) error {
	target, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
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
// run — execute a command in the alias directory.
//
// `onix run acme -- go test ./...`
//
// kong's `Cmd []string \`arg:"" passthrough:""\`` semantics let us capture
// everything after the `--` literally, which keeps quoting predictable.
// We do NOT invoke a shell here — extras are exec'd as argv directly. This
// is a deliberate change from v1, where `r acme "go test"` round-tripped
// through cmd.exe and re-parsed quoting unpredictably.
// -----------------------------------------------------------------------------

// RunCmd uses a single positional slice (rather than separate Alias+Cmd
// fields) because kong's passthrough mode requires exactly one positional
// argument on the command. We split the alias off ourselves below — a tiny
// bit of extra code in exchange for argv that passes through cleanly
// regardless of what the user types after the alias.
type RunCmd struct {
	Args []string `arg:"" name:"args" help:"<alias> <cmd> [args...]"`
}

func (c *RunCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: onix run <alias> <cmd> [args...]")
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
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return fmt.Errorf("usage: onix run <alias> <cmd> [args...]")
	}
	cmd := execCommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = e.Stdout
	cmd.Stderr = e.Stderr
	if err := cmd.Run(); err != nil {
		// Propagate child exit codes verbatim. Without this a `go test`
		// failure inside `onix run` would surface as a generic exit 1.
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// exec — run a custom action declared in config.toml.
//
// Same argv shape as run: `onix <alias> -X <action> [-- extras...]`.
// The PowerShell shell functions generated by `onix --sync` call this;
// users rarely type it directly. We separate exec from run because run
// takes an arbitrary command, whereas exec looks up a named action from
// config — different semantics, different error messages.
// -----------------------------------------------------------------------------

type ExecCmd struct {
	Args []string `arg:"" name:"args" help:"<action> <alias> [args...]"`
}

func (c *ExecCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: onix <alias> -X <action> [-- args...]")
	}
	actionName := c.Args[0]
	aliasName := c.Args[1]
	extras := c.Args[2:]
	// Same passthrough quirk as RunCmd — strip the leading "--" the
	// PowerShell wrapper inserts so action args see clean argv.
	if len(extras) > 0 && extras[0] == "--" {
		extras = extras[1:]
	}

	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}
	action := cfg.FindAction(actionName)
	if action == nil {
		return fmt.Errorf("unknown action %q (declared in %s)", actionName, config.Path(e.Home))
	}

	target, err := resolveAliasPath(e, aliasName)
	if err != nil {
		return err
	}

	argv := config.ExpandAction(action, filepath.ToSlash(target), strings.ToLower(aliasName), extras)
	if len(argv) == 0 {
		return fmt.Errorf("action %q produced empty argv", actionName)
	}

	cmd := execCommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = e.Stdout
	cmd.Stderr = e.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("exec %s %s: %w", actionName, argv[0], err)
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
	// Read both config.toml and plugins.toml first so we can list what
	// the regenerated snippet covers; RegenerateShellSnippet does the
	// actual file write.
	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}
	pf, err := plugins.LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintf(e.Stderr, "regenerated %s and wrappers in %s\n", snippet.PwshPath(e.Home), filepath.Join(e.Home, "bin"))
	} else {
		fmt.Fprintf(e.Stderr, "regenerated %s\n", snippet.BashPath(e.Home))
	}
	if len(cfg.Actions) > 0 {
		names := make([]string, 0, len(cfg.Actions))
		for _, a := range cfg.Actions {
			names = append(names, a.Name)
		}
		fmt.Fprintf(e.Stderr, "custom actions: %s\n", strings.Join(names, " "))
	}
	if len(pf.Plugins) > 0 {
		names := make([]string, 0, len(pf.Plugins))
		for _, p := range pf.Plugins {
			names = append(names, p.Name)
			for _, entry := range p.Entries {
				// Renamed from `e` to avoid shadowing the outer env parameter.
				// The inner loop only cares about the entry's wrapper name.
				names = append(names, entry.EffectiveCmd())
			}
		}
		fmt.Fprintf(e.Stderr, "plugin wrappers: %s\n", strings.Join(names, " "))
	}
	fmt.Fprintln(e.Stderr, "re-source $PROFILE (or restart PowerShell) to pick up changes")
	return nil
}

// -----------------------------------------------------------------------------
// shared helpers
// -----------------------------------------------------------------------------

// resolveAliasPath is the common prefix for every action that operates on
// the resolved directory. It uses the shared resolver to find the path
// and then ensures the directory exists on disk.
func resolveAliasPath(e *env, name string) (string, error) {
	return resolveAliasPathOpt(e, name, false)
}

func resolveAliasPathOpt(e *env, name string, noPrompt bool) (string, error) {
	var prompter func(string) string
	var selector func([]string) string
	var segPrompter resolver.SegmentPrompter

	if !noPrompt {
		prompter = func(name string) string {
			return promptDestination(name, e.Stderr, e.Stdin)
		}
		selector = func(options []string) string {
			return promptSelection(options, e.Stderr, e.Stdin)
		}
		segPrompter = func(segmentName, inlineValue string) (*segments.ContextDef, error) {
			return promptSegmentDefinition(e.Home, segmentName, inlineValue, e.Stderr, e.Stdin)
		}
	}

	p, err := resolver.Resolve(e.Home, name, prompter, selector, segPrompter)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", fmt.Errorf("create directory %q: %w", p, err)
	}
	// Record usage for frecency on every successful resolve, regardless of
	// which command triggered it (resolve/edit/yank/run/exec/...). Mirrors
	// the equivalent call in fastResolve so both code paths agree.
	_ = store.RecordUsage(e.Home, name)
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
		if _, err := exec.LookPath(e); err == nil {
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
