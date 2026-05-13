package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/plugins"
	"github.com/sadirano/onix/internal/snippet"
	"github.com/sadirano/onix/internal/store"
)

// -----------------------------------------------------------------------------
// import — pull aliases from other tools.
// -----------------------------------------------------------------------------

type ImportCmd struct {
	Zoxide bool `help:"Import from zoxide (requires 'zoxide' on PATH)."`
}

func (c *ImportCmd) Run(e *env) error {
	if !c.Zoxide {
		return fmt.Errorf("please specify a source (e.g. --zoxide)")
	}

	if c.Zoxide {
		return importZoxide(e)
	}

	return nil
}

func importZoxide(e *env) error {
	cmd := exec.Command("zoxide", "query", "-l")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("call zoxide: %w (ensure it's on PATH)", err)
	}

	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}

	lines := strings.Split(string(out), "\n")
	count := 0
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		name := strings.ToLower(filepath.Base(path))
		if name == "" || name == "." || name == "/" {
			continue
		}

		if _, exists := s.Lookup(name); exists {
			continue
		}

		if err := store.ValidateAliasName(name); err != nil {
			continue
		}

		s.Set(name, store.Alias{Path: filepath.ToSlash(path)})
		count++
	}

	if err := store.SaveStore(e.Home, s); err != nil {
		return err
	}

	fmt.Printf("imported %d aliases from zoxide\n", count)
	return nil
}


// -----------------------------------------------------------------------------
// resolve — the hot path.
//
// `onix resolve <alias>` reads aliases.toml, looks up <alias>, prints its
// absolute path to stdout, exits. The PowerShell `o` function wraps this in
// Set-Location, which is why this command must stay extremely lean: no
// directory creation, no chdir, no env mutation. Anything heavier than a
// file read + map lookup belongs in a different command.
// -----------------------------------------------------------------------------

type ResolveCmd struct {
	Alias string `arg:"" help:"Alias name (case-insensitive). Supports <seg>@<alias> segmented lookups."`
}

func (c *ResolveCmd) Run(e *env) error {
	path, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
	}
	if e.JSON {
		return printJSON(struct {
			Alias string `json:"alias"`
			Path  string `json:"path"`
		}{c.Alias, path})
	}
	fmt.Println(path)
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// -----------------------------------------------------------------------------
// add — register or update an alias.
//
// We accept either `onix add <alias> <path>` or `onix add <alias>` (uses CWD).
// The path is absolutised here so the store always holds a canonical form;
// users can later move the project and re-add without needing to remember
// what form they originally entered.
// -----------------------------------------------------------------------------

type AddCmd struct {
	Alias string `arg:"" help:"Alias name."`
	Path  string `arg:"" optional:"" help:"Directory path (default: current working directory)."`
}

func (c *AddCmd) Run(e *env) error {
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
	abs, err := filepath.Abs(expandTilde(p))
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
	s.Set(c.Alias, store.Alias{Path: filepath.ToSlash(abs)})
	if err := store.SaveStore(e.Home, s); err != nil {
		return err
	}
	// Human-readable confirmation goes to stderr so callers (the `o`
	// shell wrapper, scripts) can capture the resolved path from stdout
	// — same output contract as `onix resolve`.
	fmt.Fprintf(os.Stderr, "registered %s -> %s\n", strings.ToLower(c.Alias), abs)
	fmt.Println(abs)
	return nil
}

// -----------------------------------------------------------------------------
// remove — delete an alias.
// -----------------------------------------------------------------------------

type RemoveCmd struct {
	Alias string `arg:"" help:"Alias name."`
}

func (c *RemoveCmd) Run(e *env) error {
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
	fmt.Printf("removed %s\n", strings.ToLower(c.Alias))
	return nil
}

// -----------------------------------------------------------------------------
// list — print aliases in a stable, scannable table.
//
// We use tabwriter rather than fixed-width fmt so long names/paths align
// without truncation. JSON output comes later when we wire scripting helpers.
// -----------------------------------------------------------------------------

type ListCmd struct{}

func (c *ListCmd) Run(e *env) error {
	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}
	names := s.Names()

	if e.JSON {
		type aliasInfo struct {
			Name    string            `json:"name"`
			Path    string            `json:"path"`
			Subdirs map[string]string `json:"subdirs,omitempty"`
		}
		out := make([]aliasInfo, 0, len(names))
		for _, n := range names {
			a, _ := s.Lookup(n)
			out = append(out, aliasInfo{
				Name:    n,
				Path:    a.Path,
				Subdirs: a.Subdirs,
			})
		}
		return printJSON(out)
	}

	if len(names) == 0 {
		fmt.Println("no aliases registered (run: onix add <name> [path])")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ALIAS\tPATH")
	for _, n := range names {
		a, _ := s.Lookup(n)
		fmt.Fprintf(w, "%s\t%s\n", n, a.Path)
	}
	return w.Flush()
}

// -----------------------------------------------------------------------------
// aliases — open the aliases.toml file in your editor.
// -----------------------------------------------------------------------------

type AliasesCmd struct{}

func (c *AliasesCmd) Run(e *env) error {
	path := store.AliasesPath(e.Home)
	ed := resolveEditor()
	cmd := exec.Command(ed, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %s: %w", ed, err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// edit — open the alias directory in the user's editor.
//
// Editor precedence: $EDITOR > "nvim". We pass "." as the path so the editor
// opens the directory as a project, not as a file. We inherit stdin/stdout/
// stderr because terminal editors (nvim, vim) need a real tty.
// -----------------------------------------------------------------------------

type EditCmd struct {
	Alias string `arg:"" help:"Alias name."`
}

func (c *EditCmd) Run(e *env) error {
	target, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
	}
	ed := resolveEditor()
	cmd := exec.Command(ed, ".")
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %s: %w", ed, err)
	}
	return nil
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

func (c *ExploreCmd) Run(e *env) error {
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

func (c *YankCmd) Run(e *env) error {
	target, err := resolveAliasPath(e, c.Alias)
	if err != nil {
		return err
	}
	fmt.Println(target)
	if err := copyToClipboard(target); err != nil {
		// Non-fatal — we already printed the path so the user can copy it
		// manually. Warn so they know the clipboard step didn't work.
		fmt.Fprintf(os.Stderr, "warning: clipboard copy failed: %v\n", err)
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

func (c *RunCmd) Run(e *env) error {
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
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
// Same argv shape as run: `onix exec <action> <alias> [-- extras...]`.
// The PowerShell shell functions generated by `onix install-actions` call
// this; users rarely type it directly. We separate exec from run because
// run takes an arbitrary command, whereas exec looks up a named action
// from config — different semantics, different error messages.
// -----------------------------------------------------------------------------

type ExecCmd struct {
	Args []string `arg:"" name:"args" help:"<action> <alias> [args...]"`
}

func (c *ExecCmd) Run(e *env) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: onix exec <action> <alias> [args...]")
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

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("exec %s %s: %w", actionName, argv[0], err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// install-actions — regenerate the PowerShell snippet.
//
// Separate from `init` because users will edit config.toml multiple times
// over a session and we don't want to keep re-touching $PROFILE on each
// edit. install-actions only rewrites ~/.onix/shell/onix.ps1; the dot-source
// line in $PROFILE already points at it.
// -----------------------------------------------------------------------------

type InstallActionsCmd struct{}

func (c *InstallActionsCmd) Run(e *env) error {
	// Read both config.toml and plugins.toml first so we can list what
	// the regenerated snippet covers; regenerateShellSnippet does the
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
	fmt.Printf("regenerated %s\n", snippet.PwshPath(e.Home))
	if len(cfg.Actions) > 0 {
		names := make([]string, 0, len(cfg.Actions))
		for _, a := range cfg.Actions {
			names = append(names, a.Name)
		}
		fmt.Printf("custom actions: %s\n", strings.Join(names, " "))
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
		fmt.Printf("plugin wrappers: %s\n", strings.Join(names, " "))
	}
	fmt.Println("re-source $PROFILE (or restart PowerShell) to pick up changes")
	return nil
}

// -----------------------------------------------------------------------------
// list-names — print alias names, one per line.
//
// This is what tab-completion calls under the hood. The fast path in
// main.go bypasses kong for the most common shape (`onix list-names`),
// but the subcommand registration here keeps `onix --help` accurate.
// -----------------------------------------------------------------------------

type ListNamesCmd struct{}

func (c *ListNamesCmd) Run(e *env) error {
	s, err := store.LoadStore(e.Home)
	if err != nil {
		return err
	}
	for _, n := range s.Names() {
		fmt.Println(n)
	}
	return nil
}

// -----------------------------------------------------------------------------
// shared helpers
// -----------------------------------------------------------------------------

// resolveAliasPath is the common prefix for every action that operates on
// the resolved directory. We centralise the lookup so the error message
// is consistent and the store-read happens exactly once per command.
//
// Segmented input (`<seg>@<alias>` or longer) is delegated to the segment
// walker, which loads the global subdirs registry and respects per-alias
// overrides. Both shapes return a host-native path (FromSlash) because
// downstream Cmd.Dir and exec.Command want platform separators.
func resolveAliasPath(e *env, name string) (string, error) {
	if strings.Contains(name, "@") {
		p, err := resolveSegmentedToPath(e.Home, name)
		if err != nil {
			return "", err
		}
		return filepath.FromSlash(p), nil
	}

	s, err := store.LoadStore(e.Home)
	if err != nil {
		return "", err
	}
	a, ok := s.Lookup(name)
	if !ok {
		if err := store.ValidateAliasName(name); err != nil {
			return "", err
		}
		dest := promptDestination(name)
		if dest == "" {
			return "", fmt.Errorf("unknown alias %q (run: onix list)", name)
		}
		abs, err := filepath.Abs(expandTilde(dest))
		if err != nil {
			return "", fmt.Errorf("absolutise %q: %w", dest, err)
		}
		a = store.Alias{Path: filepath.ToSlash(abs)}
		s.Set(name, a)
		if err := store.SaveStore(e.Home, s); err != nil {
			return "", err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", fmt.Errorf("create directory %q: %w", abs, err)
		}
		fmt.Fprintf(os.Stderr, "registered %s -> %s\n", strings.ToLower(name), abs)
		return abs, nil
	}

	target := filepath.FromSlash(a.Path)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create directory %q: %w", target, err)
	}
	return target, nil
}

// resolveEditor returns the editor to invoke for `onix edit`.
// Lookup order: $EDITOR, then nvim. Trim so a trailing newline in the env
// var (which happens with naive bash exports) doesn't make exec fail.
func resolveEditor() string {
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	return "nvim"
}

// copyToClipboard pipes s into the platform clipboard utility. On Windows we
// use built-in clip.exe; Unix support comes later (pbcopy / xclip / wl-copy).
func copyToClipboard(s string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip.exe")
	default:
		return fmt.Errorf("clipboard not supported on %s yet", runtime.GOOS)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, s); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
