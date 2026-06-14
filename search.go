package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// -----------------------------------------------------------------------------
// grep — search file contents using ripgrep and fzf.
// -----------------------------------------------------------------------------

type GrepCmd struct {
	Args []string `arg:"" name:"args" help:"<alias> [query] [extras...]"`
}

func (c *GrepCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 1 {
		return fmt.Errorf("usage: onix grep <alias> [query] [extras...]")
	}
	alias := c.Args[0]
	query := ""
	if len(c.Args) > 1 {
		query = c.Args[1]
	}
	var extras []string
	if len(c.Args) > 2 {
		extras = c.Args[2:]
	}

	target, err := resolveAliasPath(e, alias)
	if err != nil {
		return err
	}

	if _, err := lookPath("rg"); err != nil {
		return fmt.Errorf("ripgrep ('rg') not found on PATH")
	}
	if _, err := lookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found on PATH")
	}

	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}

	relaxed := false
	if query != "" && !cfg.Grep.LiteralNonASCII {
		if rewritten := relaxNonASCII(query); rewritten != query {
			query = rewritten
			relaxed = true
		}
	}

	// rg output is file:line:text — fzf splits on `:`, so {1}=file and
	// {2}=line in the preview command. No trailing "." so rg prints
	// "src/foo.go:" rather than "./src/foo.go:". rgCmd.Stdin is set to
	// the parent's tty below so rg falls back to "search cwd" instead
	// of reading patterns from a nil stdin.
	rgArgs := []string{"--smart-case", "--color=always", "--line-number", "--no-heading"}
	if relaxed {
		// --no-unicode makes `.` match raw bytes, which is what lets the
		// relaxed query find accented chars stored in any encoding.
		rgArgs = append(rgArgs, "--no-unicode")
	}
	for _, spec := range cfg.Grep.RgColorsOrDefault() {
		rgArgs = append(rgArgs, "--colors", spec)
	}
	rgArgs = append(rgArgs, extras...)
	if query != "" {
		rgArgs = append(rgArgs, query)
	}

	fzfArgs := []string{"--ansi", "--multi"}
	if strings.TrimSpace(cfg.Grep.FzfColors) != "" {
		fzfArgs = append(fzfArgs, "--color", cfg.Grep.FzfColors)
	}
	fzfArgs = append(
		fzfArgs,
		"--delimiter", ":",
		"--preview", cfg.Grep.PreviewCommandOrDefault(),
		"--preview-window", cfg.Grep.PreviewWindowOrDefault(),
	)

	rgCmd := execCommandContext(ctx, "rg", rgArgs...)
	rgCmd.Dir = target
	rgCmd.Stdin = os.Stdin
	rgOut, err := rgCmd.StdoutPipe()
	if err != nil {
		return err
	}

	fzfCmd := execCommandContext(ctx, "fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = rgOut
	fzfCmd.Stderr = os.Stderr // fzf UI uses stderr when stdout is captured
	applyDefaultFzfTheme(fzfCmd)

	if err := rgCmd.Start(); err != nil {
		return fmt.Errorf("start rg: %w", err)
	}

	selected, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil // User cancelled fzf
		}
		// fzf returns exit code 1 if nothing selected, but we should distinguish between error and no selection.
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(selected)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil
	}

	return openSelectionsInEditor(ctx, target, lines, true)
}

// relaxNonASCII rewrites every non-ASCII rune in the query to the regex
// "." so a UTF-8 query matches the same position in cp1252 (or any
// other) encoding. Encoded as a single byte, accented chars don't match
// their multi-byte UTF-8 form across encodings — "." accepts whatever
// rg sees there.
func relaxNonASCII(query string) string {
	if !strings.ContainsFunc(query, func(r rune) bool { return r > 127 }) {
		return query
	}
	var b strings.Builder
	b.Grow(len(query))
	for _, r := range query {
		if r > 127 {
			b.WriteByte('.')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// find — find files using Everything/fd and fzf.
// -----------------------------------------------------------------------------

type FindCmd struct {
	Args []string `arg:"" name:"args" help:"<alias> [query] [extras...]"`
}

func (c *FindCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 1 {
		return fmt.Errorf("usage: onix find <alias> [query] [extras...]")
	}
	alias := c.Args[0]
	query := ""
	if len(c.Args) > 1 {
		query = c.Args[1]
	}
	var extras []string
	if len(c.Args) > 2 {
		extras = c.Args[2:]
	}

	target, err := resolveAliasPath(e, alias)
	if err != nil {
		return err
	}

	if _, err := lookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found on PATH")
	}

	var findCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		if _, err := lookPath("es"); err == nil {
			esArgs := []string{"-path", "./"}
			if query != "" {
				esArgs = append(esArgs, query)
			}
			esArgs = append(esArgs, extras...)
			findCmd = execCommandContext(ctx, "es", esArgs...)
			findCmd.Dir = target
		}
	}

	if findCmd == nil {
		if _, err := lookPath("fd"); err == nil {
			fdArgs := []string{"--type", "f", "--color", "always"}
			fdArgs = append(fdArgs, extras...)
			if query != "" {
				fdArgs = append(fdArgs, query)
			}
			findCmd = execCommandContext(ctx, "fd", fdArgs...)
			findCmd.Dir = target
		} else {
			// Fallback to find
			findArgs := []string{".", "-type", "f"}
			if query != "" {
				findArgs = append(findArgs, "-name", "*"+query+"*")
			}
			findArgs = append(findArgs, extras...)
			findCmd = execCommandContext(ctx, "find", findArgs...)
			findCmd.Dir = target
		}
	}

	findOut, err := findCmd.StdoutPipe()
	if err != nil {
		return err
	}

	fzfArgs := []string{
		"--ansi",
		"--multi",
		"--preview", findPreviewCommand(e.Home),
		"--preview-window", "up:40%:border-bottom",
	}
	fzfCmd := execCommandContext(ctx, "fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = findOut
	fzfCmd.Stderr = os.Stderr
	applyDefaultFzfTheme(fzfCmd)

	if err := findCmd.Start(); err != nil {
		return fmt.Errorf("start finder: %w", err)
	}

	selected, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil
		}
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(selected)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil
	}

	return openFindSelections(ctx, target, lines)
}

// defaultAppExts is the allowlist of file types `ff` opens with their OS
// default application instead of the editor. It is deliberately a
// whitelist, not a denylist of dangerous types: anything not listed —
// source, configs, and crucially executables/scripts (.cmd, .exe, .ps1) —
// falls through to the editor and never gets auto-launched. Grow it when a
// view-only type you actually open is missing, not to chase every handler.
var defaultAppExts = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".odt": true, ".rtf": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".svg": true, ".webp": true,
	".zip": true, ".7z": true, ".rar": true,
	".mp4": true, ".mkv": true, ".mov": true, ".mp3": true, ".wav": true, ".avi": true,
}

// opensWithDefaultApp reports whether a selection should open with its OS
// default app rather than the editor: a directory (open the folder) or an
// allowlisted file extension.
func opensWithDefaultApp(path string) bool {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return true
	}
	return defaultAppExts[strings.ToLower(filepath.Ext(path))]
}

// openFindSelections routes each fzf selection by type. Allowlisted files
// and directories open with the OS handler (PDF→viewer, dir→Explorer) via
// openInExplorer; everything else goes to $EDITOR. Each selection is routed
// independently, so a mixed pick (a .pdf and a .go) opens each correctly and
// nothing executable is ever launched. openInExplorer needs an absolute
// path; the editor branch keeps the original (relative) selection because
// openSelectionsInEditor runs the editor with cmd.Dir set to the alias dir.
func openFindSelections(ctx context.Context, target string, selections []string) error {
	var editorSel []string
	for _, sel := range selections {
		abs := sel
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(target, sel)
		}
		if opensWithDefaultApp(abs) {
			if err := openInExplorer(abs); err != nil {
				return err
			}
			continue
		}
		editorSel = append(editorSel, sel)
	}
	if len(editorSel) == 0 {
		return nil
	}
	return openSelectionsInEditor(ctx, target, editorSel, false)
}

// findPreviewCommand returns the fzf --preview command for `ff`. It must
// handle both files (bat) and directories (listing) since es returns
// directories alongside files and bat errors on a directory. On Windows we
// route the preview through `onix --preview`, a built-in that does the
// dir-vs-file branching in Go — inline if/else inside fzf's --preview trips
// cmd.exe's parser on Windows paths. POSIX shells parse the inline
// bat-or-ls form fine.
func findPreviewCommand(home string) string {
	if runtime.GOOS == "windows" {
		exe := filepath.Join(home, "bin", "onix.exe")
		return `"` + exe + `" --preview "{}"`
	}
	return `bat --style=numbers --color=always "{}" 2>/dev/null || ls -la "{}"`
}

// PreviewCmd renders a single fzf preview row for `ff`: a directory listing
// for directories, syntax-highlighted contents (via bat when present) for
// files. A preview must never fail the picker, so every error path degrades
// to printing a short message and returning nil.
type PreviewCmd struct {
	Path string
}

func (c *PreviewCmd) Run(ctx context.Context, e *env) error {
	p := c.Path
	if runtime.GOOS == "windows" {
		// fzf shell-escapes the substituted {} with carets on Windows; wrapped
		// in our double quotes, cmd.exe keeps them literal so they reach us in
		// argv. Strip them unconditionally (paths with literal carets never
		// previewed).
		p = strings.ReplaceAll(p, "^", "")
	}

	info, err := os.Stat(p)
	if err != nil {
		fmt.Fprintln(e.Stdout, err)
		return nil
	}

	if info.IsDir() {
		entries, err := os.ReadDir(p)
		if err != nil {
			fmt.Fprintln(e.Stdout, err)
			return nil
		}
		for _, ent := range entries {
			name := ent.Name()
			if ent.IsDir() {
				name += string(os.PathSeparator)
			}
			fmt.Fprintln(e.Stdout, name)
		}
		return nil
	}

	if _, err := lookPath("bat"); err == nil {
		cmd := execCommandContext(ctx, "bat", "--style=numbers", "--color=always", p)
		cmd.Stdout = e.Stdout
		cmd.Stderr = e.Stderr
		_ = cmd.Run()
		return nil
	}

	f, err := os.Open(p)
	if err != nil {
		fmt.Fprintln(e.Stdout, err)
		return nil
	}
	defer f.Close()
	_, _ = io.Copy(e.Stdout, f)
	return nil
}

// fzfTokyoNightTheme is the default --color set we hand fzf via
// FZF_DEFAULT_OPTS when the user hasn't set one. Whitespace (including
// newlines) is fine inside FZF_DEFAULT_OPTS — fzf treats it as an
// argument separator.
const fzfTokyoNightTheme = "" +
	"--color=fg:#c0caf5,bg:-1,hl:#2ac3de,fg+:#c0caf5,bg+:#283457 " +
	"--color=hl+:#2ac3de,info:#7aa2f7,prompt:#2ac3de,pointer:#ff007c " +
	"--color=marker:#ff5da0,spinner:#ff007c,header:#ff9e64,query:#c0caf5 " +
	"--color=border:#27a1b9,separator:#ff9e64,gutter:#283457"

// applyDefaultFzfTheme installs the Tokyo Night palette for fzf via
// FZF_DEFAULT_OPTS, but only if the parent process hasn't already set
// it — a user's own theme always wins.
func applyDefaultFzfTheme(cmd *exec.Cmd) {
	if os.Getenv("FZF_DEFAULT_OPTS") != "" {
		return
	}
	cmd.Env = append(os.Environ(), "FZF_DEFAULT_OPTS="+fzfTokyoNightTheme)
}

func openSelectionsInEditor(ctx context.Context, target string, selections []string, hasLineNumbers bool) error {
	ed := resolveEditor()
	if ed == "" {
		return fmt.Errorf("no editor found: set $EDITOR or ensure one of nvim, vim, code, nano, notepad is on PATH")
	}

	// grep selections are <relative file>:<line>:<text>; find selections
	// are just the file path. On Windows, find can return drive-letter
	// paths ("C:\..."), which themselves contain a colon — so we can't
	// detect the format from the split alone. The caller tells us.
	targets := make([]editTarget, 0, len(selections))
	for _, s := range selections {
		if hasLineNumbers {
			parts := strings.SplitN(s, ":", 3)
			if len(parts) >= 2 {
				targets = append(targets, editTarget{file: parts[0], line: parts[1]})
				continue
			}
		}
		targets = append(targets, editTarget{file: s})
	}

	// editorArgs formats the line jump in the resolved editor's dialect
	// (vim's "+line", VS Code's "--goto file:line", etc.).
	argv := editorArgs(ed, targets)
	if len(argv) == 0 {
		return nil
	}

	cmd := execCommandContext(ctx, ed, argv...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
