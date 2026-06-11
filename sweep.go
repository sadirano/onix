package main

// sweep — scan the Everything index for directories that flood the
// unknown-alias picker and exclude the worst offenders.
//
// A directory with hundreds of immediate subfolders (a photo library, a
// resource-pack tree, a node_modules cousin the defaults don't know)
// eats the picker's es -n result cap on any query its children match.
// The sweep streams every indexed directory (`es /ad`), drops ones the
// current exclusions already hide, counts direct children per parent,
// and offers the flooding parents in an fzf multi-select. Enter appends
// the marked ones to ~/.onix/picker.swept (hiding their subtrees, not
// the directory itself, where es quoting allows) and regenerates the
// wrappers; Esc changes nothing. --no-prompt prints the ranking only.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/snippet"
	"github.com/sadirano/onix/internal/store"
)

// sweepDefaultMin is the direct-subfolder count at which a directory
// counts as flooding. Override per run with --min.
const sweepDefaultMin = 100

// sweepMaxSuggestions caps the fzf list — beyond this the tail is noise,
// and every accepted entry lengthens the generated es line, which lives
// inside a single cmd.exe batch line.
const sweepMaxSuggestions = 40

type SweepCmd struct {
	Min int
}

type sweepCandidate struct {
	path  string
	count int
}

func (c *SweepCmd) Run(ctx context.Context, e *env) error {
	min := c.Min
	if min <= 0 {
		min = sweepDefaultMin
	}
	if _, err := lookPath("es"); err != nil {
		return fmt.Errorf("Everything 'es' CLI not found on PATH")
	}

	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}
	excludes, err := config.PickerExcludes(e.Home, cfg)
	if err != nil {
		return err
	}
	// Alias targets are known-good trees: never offer to hide one.
	var aliasPaths []string
	if s, err := store.LoadStore(e.Home); err == nil {
		for _, name := range s.Names() {
			if a, ok := s.Lookup(name); ok {
				aliasPaths = append(aliasPaths, store.ExpandTilde(a.Path))
			}
		}
	}

	esCmd := execCommandContext(ctx, "es", "/ad")
	esCmd.Stderr = e.Stderr
	pipe, err := esCmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := esCmd.Start(); err != nil {
		return fmt.Errorf("es: %w", err)
	}
	cands, scanErr := sweepAnalyze(pipe, excludes, aliasPaths, min)
	waitErr := esCmd.Wait()
	if scanErr != nil {
		return scanErr
	}
	if waitErr != nil {
		return fmt.Errorf("es: %w", waitErr)
	}

	if len(cands) == 0 {
		fmt.Fprintf(e.Stdout, "no directories with %d+ unfiltered subfolders found\n", min)
		return nil
	}

	var b strings.Builder
	for _, cd := range cands {
		fmt.Fprintf(&b, "%d\t%s\n", cd.count, cd.path)
	}
	if e.NoPrompt {
		fmt.Fprint(e.Stdout, b.String())
		return nil
	}
	if _, err := lookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found on PATH (use --no-prompt to just print the ranking)")
	}

	fzfCmd := execCommandContext(ctx, "fzf", "--multi", "--layout=reverse",
		"--header", "sweep: Tab marks, Enter hides marked subtrees from the picker, Esc cancels")
	fzfCmd.Stdin = strings.NewReader(b.String())
	fzfCmd.Stderr = os.Stderr // fzf UI uses stderr when stdout is captured
	applyDefaultFzfTheme(fzfCmd)

	selected, err := fzfCmd.Output()
	if err != nil {
		// Same contract as the other fzf consumers: 130 is Esc, 1 is
		// nothing-matched — both mean "hide nothing".
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}

	var frags []string
	for _, line := range strings.Split(strings.TrimSpace(string(selected)), "\n") {
		_, path, found := strings.Cut(line, "\t")
		if !found || strings.TrimSpace(path) == "" {
			continue
		}
		frags = append(frags, sweepFragment(strings.TrimSpace(path)))
	}
	if len(frags) == 0 {
		fmt.Fprintln(e.Stderr, "nothing swept")
		return nil
	}
	added, err := config.AppendSwept(e.Home, frags)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		fmt.Fprintln(e.Stderr, "nothing swept (already excluded)")
		return nil
	}
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}
	fmt.Fprintf(e.Stderr, "swept %d into %s and regenerated wrappers:\n  %s\n",
		len(added), config.SweptPath(e.Home), strings.Join(added, "\n  "))
	return nil
}

// sweepFragment turns a flooding directory into an es exclusion term.
// The trailing backslash hides the subtree while keeping the directory
// itself pickable; for paths with spaces it has to be dropped (es eats
// a backslash-quote pair), hiding the directory too.
func sweepFragment(path string) string {
	frag := strings.TrimSuffix(path, `\`) + `\`
	if strings.ContainsAny(frag, " \t") {
		return strings.TrimSuffix(frag, `\`)
	}
	return frag
}

// sweepAnalyze streams directory paths (one per line), drops ones the
// current exclusions already hide, counts direct children per parent,
// and returns the flooding parents: count >= min, at least two levels
// below the drive root, not covering any alias target. Groups of three
// or more flooding siblings collapse into their parent (a library of
// libraries — exclude the root, not each shelf). Ranked worst-first,
// capped at sweepMaxSuggestions.
func sweepAnalyze(r io.Reader, excludes, aliasPaths []string, min int) ([]sweepCandidate, error) {
	lowerEx := make([]string, len(excludes))
	for i, f := range excludes {
		lowerEx[i] = strings.ToLower(f)
	}

	counts := make(map[string]int)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
scan:
	for sc.Scan() {
		p := strings.TrimSpace(sc.Text())
		if p == "" {
			continue
		}
		lp := strings.ToLower(p)
		for _, frag := range lowerEx {
			if strings.Contains(lp, frag) {
				continue scan
			}
		}
		cut := strings.LastIndexByte(p, '\\')
		if cut <= 0 {
			continue
		}
		counts[p[:cut]]++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading es output: %w", err)
	}

	var cands []sweepCandidate
	for parent, n := range counts {
		if n >= min && strings.Count(parent, `\`) >= 2 {
			cands = append(cands, sweepCandidate{path: parent, count: n})
		}
	}

	// Sibling collapse, to a fixpoint: three+ flooding children of one
	// parent become the parent itself.
	for changed := true; changed; {
		changed = false
		byParent := make(map[string][]int)
		for i, cd := range cands {
			if cut := strings.LastIndexByte(cd.path, '\\'); cut > 0 {
				parent := cd.path[:cut]
				byParent[parent] = append(byParent[parent], i)
			}
		}
		for parent, kids := range byParent {
			if len(kids) < 3 || strings.Count(parent, `\`) < 2 {
				continue
			}
			sum := 0
			drop := make(map[int]struct{}, len(kids))
			for _, i := range kids {
				sum += cands[i].count
				drop[i] = struct{}{}
			}
			next := cands[:0]
			for i, cd := range cands {
				if _, gone := drop[i]; !gone {
					next = append(next, cd)
				}
			}
			cands = append(next, sweepCandidate{path: parent, count: sum})
			changed = true
			break // indices invalidated — regroup from scratch
		}
	}

	// Never offer a directory that contains (or is) an alias target.
	kept := cands[:0]
	for _, cd := range cands {
		prefix := strings.ToLower(strings.TrimSuffix(cd.path, `\`)) + `\`
		covers := false
		for _, ap := range aliasPaths {
			lap := strings.ToLower(strings.TrimSuffix(ap, `\`)) + `\`
			if strings.HasPrefix(lap, prefix) {
				covers = true
				break
			}
		}
		if !covers {
			kept = append(kept, cd)
		}
	}
	cands = kept

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].count != cands[j].count {
			return cands[i].count > cands[j].count
		}
		return cands[i].path < cands[j].path
	})
	if len(cands) > sweepMaxSuggestions {
		cands = cands[:sweepMaxSuggestions]
	}
	return cands, nil
}
