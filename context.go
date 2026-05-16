package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/sadirano/onix/internal/segments"
)

// ContextCmd groups the segment-context subcommands under `onix context`.
//
// The primary consumer is the `o` shell function which calls
// `onix context apply <alias>` after every Set-Location. The list/edit
// commands are for humans managing their segments.toml.
type ContextCmd struct {
	Apply ContextApplyCmd `cmd:"" name:"apply" help:"Print PowerShell context statements for a segmented alias (called by the o shell function)."`
	List  ContextListCmd  `cmd:"" name:"list" help:"List all segment contexts defined in segments.toml."`
	Edit  ContextEditCmd  `cmd:"" name:"edit" help:"Open segments.toml in your editor."`
}

// ContextApplyCmd is the hot-path command invoked by the `o` shell function
// after every cd. For plain aliases (no '@') it prints nothing. For segmented
// aliases it prints PowerShell statements that the caller evaluates via
// Invoke-Expression — setting env vars and running any post-cd exec command.
type ContextApplyCmd struct {
	Alias string `arg:"" help:"Alias (plain or segmented). Plain aliases produce no output."`
	Shell string `help:"Output shell format (pwsh, bash, zsh). Defaults to 'pwsh'." default:"pwsh"`
}

func (c *ContextApplyCmd) Run(ctx context.Context, e *env) error {
	return applyContexts(e.Home, c.Alias, c.Shell, os.Stdout)
}

// ContextListCmd prints every context defined in segments.toml in a
// scannable table: segment name, env keys (sorted), exec command.
type ContextListCmd struct{}

func (c *ContextListCmd) Run(ctx context.Context, e *env) error {
	sf, err := segments.LoadSegments(e.Home)
	if err != nil {
		return err
	}
	if len(sf.Contexts) == 0 {
		fmt.Println("(no contexts defined — add [[contexts]] blocks to ~/.onix/segments.toml)")
		fmt.Println("run: onix context edit")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEGMENT\tENV\tEXEC")
	for _, cd := range sf.Contexts {
		envKeys := make([]string, 0, len(cd.Env))
		for k := range cd.Env {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		envStr := "-"
		if len(envKeys) > 0 {
			envStr = strings.Join(envKeys, ", ")
		}
		execStr := "-"
		if len(cd.Exec) > 0 {
			execStr = strings.Join(cd.Exec, " ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", cd.Segment, envStr, execStr)
	}
	return w.Flush()
}

// ContextEditCmd opens segments.toml in the user's $EDITOR, creating the
// file with a commented starter template if it doesn't exist yet.
type ContextEditCmd struct{}

func (c *ContextEditCmd) Run(ctx context.Context, e *env) error {
	p := segments.Path(e.Home)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		const starter = `# onix segment registry — @-segment subdirs and shell contexts.
# After editing, changes are picked up immediately (no reload needed).
#
# [subdirs]
# docs = "documentation"
# src  = "source"
#
# [[contexts]]
# segment = "src"
# env     = { GO111MODULE = "on", GOFLAGS = "-tags=integration" }
# exec    = ["make", "dev-env"]
`
		if err := os.WriteFile(p, []byte(starter), 0o644); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
	ed := resolveEditor()
	cmd := exec.CommandContext(ctx, ed, p)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %s: %w", ed, err)
	}
	return nil
}

// applyContexts is the core of `onix context apply`. It is kept as a
// standalone function (rather than inlined in Run) so the hot-path bypass
// in main.go can call it directly without going through kong.
//
// For plain aliases (no '@') it returns immediately — no file I/O, no
// allocations. For segmented aliases it loads segments.toml, finds any
// matching [[contexts]] entries, and writes shell env-var setters
// and exec invocations to w in innermost-first segment order.
func applyContexts(home, input, shell string, w io.Writer) error {
	if !strings.Contains(input, "@") {
		return nil // plain alias — no context possible, skip all I/O
	}
	segs, _ := segments.ParseSegmentedAlias(input)
	if len(segs) == 0 {
		return nil
	}
	sf, err := segments.LoadSegments(home)
	if err != nil {
		return err
	}
	if len(sf.Contexts) == 0 {
		return nil
	}

	// Build a segment→ContextDef lookup. First-defined wins for duplicates
	// so the TOML author controls precedence by ordering their [[contexts]].
	ctxMap := make(map[string]*segments.ContextDef, len(sf.Contexts))
	for i := range sf.Contexts {
		cd := &sf.Contexts[i]
		key := strings.ToLower(cd.Segment)
		if _, exists := ctxMap[key]; !exists {
			ctxMap[key] = cd
		}
	}

	isBash := shell == "bash" || shell == "zsh"
	isCmd := shell == "cmd"

	// Apply in innermost-first order (right-to-left in the segments slice)
	// to mirror the path-building direction.
	for i := len(segs) - 1; i >= 0; i-- {
		cd, ok := ctxMap[strings.ToLower(segs[i])]
		if !ok {
			continue
		}
		// Env vars in sorted key order for deterministic output.
		keys := make([]string, 0, len(cd.Env))
		for k := range cd.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if isBash {
				fmt.Fprintf(w, "export %s=%s\n", k, shQuote(cd.Env[k]))
			} else if isCmd {
				fmt.Fprintf(w, "set %s=%s\n", k, cd.Env[k])
			} else {
				fmt.Fprintf(w, "$env:%s = %s\n", k, psSingleQuote(cd.Env[k]))
			}
		}
		// Exec: each argument individually quoted.
		if len(cd.Exec) > 0 {
			if isCmd {
				fmt.Fprintf(w, "%s\n", strings.Join(cd.Exec, " "))
				continue
			}
			quoted := make([]string, len(cd.Exec))
			for j, arg := range cd.Exec {
				if isBash {
					quoted[j] = shQuote(arg)
				} else {
					quoted[j] = psSingleQuote(arg)
				}
			}
			if isBash {
				fmt.Fprintf(w, "%s\n", strings.Join(quoted, " "))
			} else {
				fmt.Fprintf(w, "& %s\n", strings.Join(quoted, " "))
			}
		}
	}
	return nil
}

// shQuote wraps s in single quotes for POSIX shells, escaping any embedded
// single quotes by ending the string, adding an escaped quote, then restarting.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psSingleQuote wraps s in PowerShell single quotes, escaping any embedded
// single quotes by doubling them (the only escape in a PS literal string).
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
