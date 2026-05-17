package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/sadirano/onix/internal/segments"
)

// ContextListCmd prints every context defined in segments.toml in a
// scannable table: segment name, env keys (sorted), exec command.
// Invoked via `onix --contexts`.
type ContextListCmd struct{}

func (c *ContextListCmd) Run(ctx context.Context, e *env) error {
	sf, err := segments.LoadSegments(e.Home)
	if err != nil {
		return err
	}
	if len(sf.Contexts) == 0 {
		fmt.Fprintln(e.Stdout, "(no contexts defined — add [[contexts]] blocks to ~/.onix/segments.toml)")
		fmt.Fprintln(e.Stdout, "run: onix --edit segments.toml")
		return nil
	}
	w := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
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

// applyContexts is the core of `onix --apply-context`. It is called by
// both the dispatcher and the `o` shell function after every Set-Location.
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
		cd, ok := ctxMap[strings.ToLower(segs[i].Name)]
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
