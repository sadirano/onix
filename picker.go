package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/resolver"
)

// pickDirectory handles an unknown alias: when a command names no known
// alias, it lists candidate directories with Everything (`es`), lets the
// user choose one in fzf, registers the alias against the pick, and returns
// the chosen path so the caller can act on it.
//
// onix drives es and fzf in-process, so nothing is piped through a shared
// state file — an abandoned picker can only ever block itself, never global
// navigation. A cancelled pick yields resolver.ErrCancelled, which the
// dispatcher reports silently.
func pickDirectory(ctx context.Context, e *env, name string) (string, error) {
	if _, err := lookPath("es"); err != nil {
		return "", fmt.Errorf("unknown alias %q (install the Everything 'es' CLI for the directory picker, or register it: onix %s <path>)", name, name)
	}
	if _, err := lookPath("fzf"); err != nil {
		return "", fmt.Errorf("unknown alias %q (install fzf for the directory picker, or register it: onix %s <path>)", name, name)
	}

	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return "", err
	}
	excludes, err := config.PickerExcludes(e.Home, cfg)
	if err != nil {
		return "", err
	}

	// Ask es only for directories matching the name; apply the exclusions in
	// Go rather than as `!path:` query terms. es's modifier quoting does not
	// survive Go's argv-to-command-line reconstruction (a spaced exclusion
	// like "C:\Program Files" gets quoted as one token and es then treats the
	// whole `!path:...` as a literal search term, zeroing the result set), so
	// filtering here is both correct and simpler. The raw cap is generous
	// because many candidates get filtered out before fzf sees them.
	esCmd := execCommandContext(ctx, "es", name, "/ad", "-n", "5000")
	raw, err := esCmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return "", fmt.Errorf("run es: %w", err)
		}
		// Non-zero exit is treated as "no matches" below.
	}

	candidates := filterExcludedDirs(strings.Split(string(raw), "\n"), excludes)
	if len(candidates) == 0 {
		fmt.Fprintf(e.Stderr, "no unregistered directory matches %q (register it directly: onix %s <path>)\n", name, name)
		return "", resolver.ErrCancelled
	}
	const maxCandidates = 500
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	fzfCmd := execCommandContext(
		ctx, "fzf",
		"--preview", findPreviewCommand(e.Home),
		"--preview-window", "right:50%:border-left",
	)
	fzfCmd.Stdin = strings.NewReader(strings.Join(candidates, "\n"))
	fzfCmd.Stderr = os.Stderr // fzf UI uses stderr when stdout is captured
	applyDefaultFzfTheme(fzfCmd)

	selected, err := fzfCmd.Output()
	if err != nil {
		// fzf exits non-zero on cancel (130) or empty selection (1); both mean
		// "user chose nothing", which we treat as a silent cancel.
		if _, ok := err.(*exec.ExitError); ok {
			return "", resolver.ErrCancelled
		}
		return "", fmt.Errorf("fzf: %w", err)
	}

	pick := strings.TrimSpace(string(selected))
	if pick == "" {
		return "", resolver.ErrCancelled
	}

	// Register name -> pick (AddCmd absolutises, creates the dir, and records
	// usage). Its stdout path line and stderr "registered" note are the same
	// confirmation the user got from the old flow.
	if err := (&AddCmd{Alias: name, Path: pick}).Run(ctx, e); err != nil {
		return "", err
	}
	return pick, nil
}

// filterExcludedDirs drops blank lines and any path containing an exclusion
// fragment, case-insensitively — the same substring semantics es applied for
// `!path:<frag>`, reproduced in Go.
func filterExcludedDirs(lines, excludes []string) []string {
	lowered := make([]string, 0, len(excludes))
	for _, frag := range excludes {
		if frag != "" {
			lowered = append(lowered, strings.ToLower(frag))
		}
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		p := strings.TrimRight(line, "\r")
		if strings.TrimSpace(p) == "" {
			continue
		}
		lp := strings.ToLower(p)
		excluded := false
		for _, frag := range lowered {
			if strings.Contains(lp, frag) {
				excluded = true
				break
			}
		}
		if !excluded {
			out = append(out, p)
		}
	}
	return out
}
