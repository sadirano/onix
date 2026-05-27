package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/sadirano/onix/internal/segments"
)

// ContextListCmd prints every context defined in segments.toml in a
// scannable table: segment name, env keys, and the source field that
// produces the path fragment. Invoked via `onix --contexts`.
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
	fmt.Fprintln(w, "SEGMENT\tENV\tSOURCE")
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
		fmt.Fprintf(w, "%s\t%s\t%s\n", cd.Segment, envStr, sourceSummary(cd))
	}
	return w.Flush()
}

func sourceSummary(cd segments.ContextDef) string {
	if cd.SourceTemplate != "" {
		return "template=" + cd.SourceTemplate
	}
	return "-"
}
