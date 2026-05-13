package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/snippet"
	"github.com/sadirano/onix/internal/store"
)

// starterAliases is the placeholder aliases.toml written on first init.
// We keep an explicit file (with just a comment) so `onix list` and the
// hot path read the same "empty" state instead of falling through to the
// not-found branch on every invocation until the first `onix add`.
const starterAliases = `# onix aliases — edit with care, prefer 'onix add' / 'onix rm'
`

// starterConfig is the placeholder config.toml written on first init.
// Empty (just a comment + a worked example) so the file exists for the
// user to extend, but no actions are declared yet — so the snippet has
// only the built-in functions.
const starterConfig = `# onix configuration — declare custom actions here.
# After editing, run: onix install-actions
#
# Example:
#
#   [[actions]]
#   name = "test"
#   exec = "go"
#   args = ["test", "./..."]
#
#   [[actions]]
#   name = "pr"
#   exec = "gh"
#   args = ["pr", "view", "{extras}", "--web"]
`

// InitCmd creates ~/.onix and installs PowerShell shell integration.
type InitCmd struct {
	SkipProfile bool `help:"Don't modify the PowerShell $PROFILE." name:"skip-profile"`
}

func (c *InitCmd) Run(e *env) error {
	// 1. directory tree
	if err := os.MkdirAll(filepath.Join(e.Home, "shell"), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", e.Home, err)
	}

	// 2. config + aliases starters — only write if missing.
	cfgPath := config.Path(e.Home)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfgPath, []byte(starterConfig), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", cfgPath, err)
		}
	}
	aliases := store.AliasesPath(e.Home)
	if _, err := os.Stat(aliases); os.IsNotExist(err) {
		if err := os.WriteFile(aliases, []byte(starterAliases), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", aliases, err)
		}
	}

	// 3. Regenerate the snippet.
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}

	fmt.Printf("onix home: %s\n", e.Home)
	fmt.Printf("shell snippet: %s\n", snippet.PwshPath(e.Home))

	// 4. $PROFILE wiring.
	if c.SkipProfile {
		fmt.Println("skipped $PROFILE update (re-run without --skip-profile to enable)")
		return nil
	}
	if runtime.GOOS != "windows" {
		return sourceFromBashLike(snippet.BashPath(e.Home))
	}

	return sourceFromProfile(snippet.PwshPath(e.Home))
}

// sourceFromBashLike appends a source line to .bashrc and/or .zshrc.
func sourceFromBashLike(snippet string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// We look for both .bashrc and .zshrc. If they exist, we append the source
	// line. This is broader than checking $SHELL but ensures that a user
	// who switches between shells is covered.
	files := []string{".bashrc", ".zshrc"}
	var updated []string
	var found bool

	// We use [ -f ... ] so the rc file doesn't error if onix is uninstalled
	// but the source line remains.
	sourceLine := fmt.Sprintf("[ -f '%s' ] && . '%s'", snippet, snippet)

	for _, f := range files {
		p := filepath.Join(home, f)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		found = true

		existing, _ := os.ReadFile(p)
		if strings.Contains(string(existing), snippet) {
			fmt.Printf("%s already sources %s\n", f, snippet)
			continue
		}

		file, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not open %s: %v\n", p, err)
			continue
		}
		if _, err := fmt.Fprintf(file, "\n# Added by 'onix init'\n%s\n", sourceLine); err != nil {
			file.Close()
			return fmt.Errorf("append to %s: %w", f, err)
		}
		file.Close()
		updated = append(updated, f)
	}

	if len(updated) > 0 {
		fmt.Printf("updated: %s\n", strings.Join(updated, ", "))
		fmt.Println("restart your shell (or source the updated file) to activate o/n/s/r/y")
	} else if !found {
		fmt.Printf("no .bashrc or .zshrc found — add this to your shell rc manually:\n  %s\n", sourceLine)
	}
	return nil
}

// sourceFromProfile appends a `. "<snippet>"` line to PowerShell's $PROFILE
// if it's not already present. We invoke powershell.exe to read $PROFILE
// rather than editing the registry — modifying user-owned config files is
// much less invasive than the v1 PATH-mutation flow.
func sourceFromProfile(snippet string) error {
	out, err := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive",
		"-Command", "$PROFILE.CurrentUserAllHosts").Output()
	if err != nil {
		return fmt.Errorf("locate $PROFILE: %w (add manually: . %q)", err, snippet)
	}
	profilePath := strings.TrimSpace(string(out))
	if profilePath == "" {
		return fmt.Errorf("powershell did not return a profile path (add manually: . %q)", snippet)
	}

	// PowerShell single-quoted strings are literal — backslashes don't need
	// escaping and the only special char is a quote, which doubles as its
	// own escape. Using single quotes here keeps Windows paths legible
	// instead of turning C:\foo into C:\\foo via Go's %q.
	sourceLine := fmt.Sprintf(". '%s'", strings.ReplaceAll(snippet, `'`, `''`))

	// Read existing content (may not exist yet). We look for the snippet
	// path rather than the exact source line so a user who hand-edited the
	// dot-source to use double quotes still trips the "already installed"
	// branch instead of getting a duplicate line.
	existing, _ := os.ReadFile(profilePath)
	if strings.Contains(string(existing), snippet) {
		fmt.Printf("$PROFILE already sources %s\n", snippet)
		return nil
	}

	// Make sure the parent dir exists — first-time PowerShell users won't
	// have a Documents\WindowsPowerShell\ directory yet.
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}

	f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open $PROFILE %s: %w", profilePath, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n# Added by 'onix init'\n%s\n", sourceLine); err != nil {
		return fmt.Errorf("append to $PROFILE: %w", err)
	}
	fmt.Printf("updated $PROFILE: %s\n", profilePath)
	fmt.Println("restart PowerShell (or run: . $PROFILE) to activate o/n/s/r/y and custom actions")
	return nil
}
