package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// gitClone clones repo into dir. `repo` may be:
//
//   - "user/repo" — treated as a GitHub URL (https://github.com/user/repo.git)
//   - "https://…" or "git@…" — used as-is
//   - an absolute local path (C:\foo, /foo, or file://…) — used as a local
//     source. Local sources are mainly for plugin development and the
//     smoke test, where we can't rely on outbound network access.
//
// Always uses --depth=50 plus --no-single-branch so subsequent `git
// checkout` of arbitrary SHAs has a chance of finding the object without
// a full fetch. We assume git is on PATH; doctor surfaces missing.
func gitClone(repo, dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dir), err)
	}
	url := resolveRepoURL(repo)
	cmd := exec.Command("git", "clone", "--depth=50", "--no-single-branch", url, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// resolveRepoURL turns the user's repo argument into something git clone
// understands. Heuristics in order: URL scheme → use as-is; drive letter
// or leading slash → use as local path; otherwise treat as user/repo on
// GitHub. Wrapped so plugin add and the install confirmation can both
// reason about the source.
func resolveRepoURL(repo string) string {
	r := strings.TrimSpace(repo)
	if strings.Contains(r, "://") {
		return r // explicit scheme (https, git, file)
	}
	// Windows drive letter (C:\…) or Unix absolute path (/foo) → local.
	if len(r) >= 3 && r[1] == ':' && (r[2] == '\\' || r[2] == '/') {
		return r
	}
	if strings.HasPrefix(r, "/") || strings.HasPrefix(r, `\\`) {
		return r
	}
	return "https://github.com/" + normalizeRepo(r) + ".git"
}

// gitFetch updates an existing checkout. We do a deep-ish fetch (50
// commits) so the SHA the user wants to pin to is reachable without
// pulling the entire history; rare older SHAs may need a manual
// `git fetch --unshallow` from the plugin directory.
func gitFetch(dir string) error {
	return runGit(dir, "fetch", "--depth=50", "origin")
}

// gitCheckout points HEAD at ref (a SHA, tag, or branch). When ref is
// empty we fast-forward the default branch — that's the --unpinned case.
// We `reset --hard` so the working tree always matches the recorded ref,
// even if a previous build left local edits behind.
func gitCheckout(dir, ref string) error {
	if ref == "" {
		// Default branch follow. Resolve HEAD's symbolic ref and fast-forward
		// to whatever origin has. Avoids hard-coding "main" vs "master".
		out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
		if err != nil {
			return fmt.Errorf("resolve default branch: %w", err)
		}
		branch := strings.TrimSpace(string(out))
		if err := runGit(dir, "reset", "--hard", "origin/"+branch); err != nil {
			return fmt.Errorf("update %s to origin/%s: %w", branch, branch, err)
		}
		return nil
	}
	if err := runGit(dir, "reset", "--hard", ref); err != nil {
		return fmt.Errorf("checkout %s: %w", ref, err)
	}
	return nil
}

// gitHeadSHA returns the current HEAD's full SHA. We use this both for
// confirmation display and for pinning when the user passed --unpinned
// (in that case we record the SHA we *built* even though we won't enforce
// it on subsequent updates).
func gitHeadSHA(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitHeadMessage returns the first line of HEAD's commit message. Short
// enough to fit in the confirmation block without overwhelming.
func gitHeadMessage(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").Output()
	if err != nil {
		return "", fmt.Errorf("read commit message: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// runGit is the shared spawn helper for git subcommands. We always wire
// stdout/stderr through so progress and errors flow to the user; this is
// strictly an admin path, so we don't worry about buffering.
func runGit(dir string, args ...string) error {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildPlugin runs `go build -o <bin> .` inside srcDir. We pin the output
// name so callers don't have to guess what go-build chose for the binary
// path; the convention matches pluginBinaryName(repo).
//
// Build flags mirror what we apply to onix itself: -trimpath and -s -w.
// Smaller binaries and faster process spawn for the same reasons.
func buildPlugin(srcDir, binaryName string) error {
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
		return fmt.Errorf("no go.mod in %s — only Go plugins are supported", srcDir)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", binaryName, ".")
	cmd.Dir = srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build in %s: %w", srcDir, err)
	}
	return nil
}

// pluginManifest is the shape of `onix.toml` inside a plugin repo. We
// keep it private to this file because it's only used at install time —
// once entries are cached into our own plugins.toml we don't reread the
// plugin's manifest.
type pluginManifest struct {
	Entries []PluginEntry `toml:"entry"`
}

// readPluginManifest loads a plugin's onix.toml when present. Missing
// file → no entries → single-entry plugin (the plugin's main binary
// itself is the wrapper). Bad TOML is a real error: the plugin author
// shipped something we can't parse, and we'd rather fail install than
// silently install with a broken entry list.
func readPluginManifest(srcDir string) ([]PluginEntry, error) {
	p := filepath.Join(srcDir, "onix.toml")
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	m := &pluginManifest{}
	if err := toml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return m.Entries, nil
}

// confirmInstall prints a multi-line summary and waits for [y/N]. Returns
// true when the user confirms; false when they decline or hit Ctrl+C.
// We accept `--yes` from the caller to skip this entirely — that's the
// path automation should take.
func confirmInstall(repo, name, sha, message string, entries []PluginEntry, unpinned bool) bool {
	fmt.Println()
	fmt.Printf("  repo:    https://github.com/%s\n", repo)
	fmt.Printf("  wrapper: %s\n", name)
	if unpinned {
		// Go 1.21+'s builtin min(int, int) gives us the SHA prefix without
		// importing a helper or risking an out-of-range slice on a short
		// `git rev-parse` output.
		fmt.Printf("  pin:     UNPINNED (tracks default branch — %s)\n", sha[:min(12, len(sha))])
	} else {
		fmt.Printf("  sha:     %s\n", sha)
	}
	if message != "" {
		fmt.Printf("  commit:  %s\n", message)
	}
	if len(entries) > 0 {
		cmds := make([]string, len(entries))
		for i, e := range entries {
			cmds[i] = e.EffectiveCmd()
		}
		fmt.Printf("  entries: %s\n", strings.Join(cmds, ", "))
	}
	fmt.Println()
	if unpinned {
		fmt.Println("  Warning: unpinned plugins rebuild from the default branch on every `onix plugin update`")
		fmt.Println("  without re-prompting. Pin to a SHA for security-sensitive use.")
		fmt.Println()
	}
	fmt.Print("  Build and install? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	ans, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(ans), "y")
}

