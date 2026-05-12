package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

// PluginCmd is the umbrella for plugin subcommands. Each child is its
// own kong command struct so help and validation stay localised.
type PluginCmd struct {
	Add    PluginAddCmd    `cmd:"" help:"Install a plugin from a GitHub repo."`
	List   PluginListCmd   `cmd:"" aliases:"ls" help:"List installed plugins."`
	Update PluginUpdateCmd `cmd:"" help:"Refetch and rebuild plugins."`
	Remove PluginRemoveCmd `cmd:"" aliases:"rm" help:"Uninstall a plugin."`
}

// PluginAddCmd installs (or reinstalls) a plugin from GitHub. The
// security posture: --sha is required unless --unpinned is passed; user
// confirms each build unless --yes is given. Re-adding an existing plugin
// replaces it (after the same confirmation flow), so users don't need a
// separate `replace` verb.
type PluginAddCmd struct {
	Repo     string `arg:"" help:"GitHub repo path (user/repo)."`
	SHA      string `name:"sha" help:"Pin to this commit SHA (required unless --unpinned)."`
	Unpinned bool   `name:"unpinned" help:"Track the default branch instead of pinning to a SHA."`
	Name     string `name:"name" help:"Wrapper command name. Defaults to repo basename without 'onix-' prefix."`
	Yes      bool   `name:"yes" short:"y" help:"Skip the confirmation prompt."`
}

func (c *PluginAddCmd) Run(e *env) error {
	if strings.TrimSpace(c.SHA) == "" && !c.Unpinned {
		return fmt.Errorf("either --sha <hash> or --unpinned is required")
	}

	repo := normalizeRepo(c.Repo)
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = defaultWrapperName(repo)
	}

	// Load current state so we can detect collisions *before* we touch
	// the filesystem. Otherwise a name conflict would only fire after
	// we'd already cloned and built.
	pf, err := LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig(e.Home)
	if err != nil {
		return err
	}

	// Clone or update. We always do the clone/fetch before SHA validation
	// because the SHA may legitimately not exist locally yet.
	srcDir := pluginSourceDir(e.Home, repo)
	if _, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil {
		if err := gitFetch(srcDir); err != nil {
			return fmt.Errorf("fetch %s: %w", repo, err)
		}
	} else {
		if err := gitClone(repo, srcDir); err != nil {
			return fmt.Errorf("clone %s: %w", repo, err)
		}
	}

	// Check out the requested ref (or update default-branch HEAD).
	ref := c.SHA
	if c.Unpinned {
		ref = ""
	}
	if err := gitCheckout(srcDir, ref); err != nil {
		return err
	}

	// Resolve actual SHA, commit subject, and manifest entries so the
	// confirmation block has everything to display.
	sha, err := gitHeadSHA(srcDir)
	if err != nil {
		return err
	}
	msg, _ := gitHeadMessage(srcDir)
	entries, err := readPluginManifest(srcDir)
	if err != nil {
		return err
	}

	// Build a candidate plugin record so we can collision-check before
	// asking the user to confirm. Removing any prior entry first lets a
	// re-add succeed without the validator complaining about itself.
	pf.Remove(name)
	candidate := Plugin{
		Name:    name,
		Repo:    repo,
		Entries: entries,
	}
	if c.Unpinned {
		candidate.Unpinned = true
		candidate.SHA = sha // record what we built even when not enforcing
	} else {
		candidate.SHA = sha
	}
	probe := &PluginsFile{Plugins: append(pf.Plugins, candidate)}
	if err := validatePlugins(probe, cfg.Actions); err != nil {
		return err
	}

	// Confirm with the user.
	if !c.Yes && !confirmInstall(repo, name, sha, msg, entries, c.Unpinned) {
		return fmt.Errorf("aborted by user")
	}

	// Build the binary. We pass the basename (not the full path) because
	// `go build -o` resolves relative paths against the build dir.
	fmt.Printf("Building %s...\n", repo)
	if err := buildPlugin(srcDir, pluginBinaryName(repo)); err != nil {
		return err
	}

	// Commit the change to plugins.toml and regenerate the snippet so
	// the new wrapper is available the moment the user re-sources $PROFILE.
	pf.Plugins = append(pf.Plugins, candidate)
	if err := SavePlugins(e.Home, pf); err != nil {
		return err
	}
	if err := regenerateShellSnippet(e.Home); err != nil {
		return err
	}

	fmt.Printf("\nInstalled %s -> %s\n", name, pluginBinaryPath(e.Home, repo))
	fmt.Println("Re-source $PROFILE (or restart PowerShell) to activate.")
	return nil
}

// PluginListCmd prints installed plugins with their SHA, repo, and
// install state. The install-state check ("installed" vs "missing binary")
// is what catches a moved or deleted ~/.onix/plugins/ directory.
type PluginListCmd struct{}

func (c *PluginListCmd) Run(e *env) error {
	pf, err := LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	if len(pf.Plugins) == 0 {
		fmt.Println("no plugins installed (run: onix plugin add <repo> --sha <hash>)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPO\tSHA\tSTATE")
	for _, p := range pf.Plugins {
		state := "installed"
		if _, err := os.Stat(pluginBinaryPath(e.Home, p.Repo)); err != nil {
			state = "missing binary"
		}
		if p.Unpinned {
			state += " (unpinned)"
		}
		sha := p.SHA
		if len(sha) > 12 {
			sha = sha[:12]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, p.Repo, sha, state)
	}
	return w.Flush()
}

// PluginUpdateCmd re-fetches and rebuilds plugins. By default it
// reinstalls every plugin at its recorded SHA (or at HEAD for unpinned
// ones). Naming a single plugin restricts the operation; `--sha <new>`
// bumps the pin to a different commit and re-confirms with the user.
type PluginUpdateCmd struct {
	Name string `arg:"" optional:"" help:"Plugin name (omit to update all)."`
	SHA  string `name:"sha" help:"Bump the plugin's pin to a new SHA (requires Name)."`
	Yes  bool   `name:"yes" short:"y" help:"Skip the confirmation prompt."`
}

func (c *PluginUpdateCmd) Run(e *env) error {
	pf, err := LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	if len(pf.Plugins) == 0 {
		fmt.Println("no plugins installed")
		return nil
	}
	if c.SHA != "" && c.Name == "" {
		return fmt.Errorf("--sha requires a plugin name (which plugin to re-pin?)")
	}

	cfg, err := LoadConfig(e.Home)
	if err != nil {
		return err
	}

	// Build the work list: either one named plugin or all of them.
	var work []*Plugin
	if c.Name != "" {
		p := pf.FindPlugin(c.Name)
		if p == nil {
			return fmt.Errorf("unknown plugin %q", c.Name)
		}
		work = append(work, p)
	} else {
		for i := range pf.Plugins {
			work = append(work, &pf.Plugins[i])
		}
	}

	for _, p := range work {
		fmt.Printf("Updating %s (%s)...\n", p.Name, p.Repo)
		srcDir := pluginSourceDir(e.Home, p.Repo)
		if err := gitFetch(srcDir); err != nil {
			return fmt.Errorf("fetch %s: %w", p.Repo, err)
		}

		ref := p.SHA
		if c.SHA != "" {
			ref = c.SHA
		}
		if p.Unpinned && c.SHA == "" {
			ref = "" // follow default branch
		}
		if err := gitCheckout(srcDir, ref); err != nil {
			return err
		}

		newSHA, err := gitHeadSHA(srcDir)
		if err != nil {
			return err
		}

		// When the SHA hasn't moved we still rebuild (so a Go-toolchain
		// update or a deleted binary heals on `update`). When it has
		// moved, re-confirm — silent bumps are how plugin trojans hide.
		if !p.Unpinned && newSHA != p.SHA {
			msg, _ := gitHeadMessage(srcDir)
			entries, err := readPluginManifest(srcDir)
			if err != nil {
				return err
			}
			if !c.Yes && !confirmInstall(p.Repo, p.Name, newSHA, msg, entries, false) {
				return fmt.Errorf("aborted update of %s", p.Name)
			}
			p.SHA = newSHA
			p.Entries = entries
		}

		if err := buildPlugin(srcDir, pluginBinaryName(p.Repo)); err != nil {
			return err
		}
	}

	if err := validatePlugins(pf, cfg.Actions); err != nil {
		return err
	}
	if err := SavePlugins(e.Home, pf); err != nil {
		return err
	}
	if err := regenerateShellSnippet(e.Home); err != nil {
		return err
	}
	fmt.Println("Re-source $PROFILE (or restart PowerShell) if entries changed.")
	return nil
}

// PluginRemoveCmd uninstalls a plugin: deletes its source tree and binary,
// strips it from plugins.toml, and regenerates the snippet so the
// shell wrappers go away.
type PluginRemoveCmd struct {
	Name string `arg:"" help:"Plugin name."`
}

func (c *PluginRemoveCmd) Run(e *env) error {
	pf, err := LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	p := pf.FindPlugin(c.Name)
	if p == nil {
		return fmt.Errorf("unknown plugin %q", c.Name)
	}

	srcDir := pluginSourceDir(e.Home, p.Repo)
	if err := os.RemoveAll(srcDir); err != nil {
		// Not fatal — the entry comes out of plugins.toml either way.
		// The user will see the directory hang around and can delete it
		// manually if they care; doctor would flag it later.
		fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", srcDir, err)
	}
	pf.Remove(c.Name)
	if err := SavePlugins(e.Home, pf); err != nil {
		return err
	}
	if err := regenerateShellSnippet(e.Home); err != nil {
		return err
	}
	fmt.Printf("Removed %s\n", c.Name)
	return nil
}

// -----------------------------------------------------------------------------
// plugin-exec — runtime dispatcher invoked by the generated shell wrappers.
//
// Argv shape: `onix plugin-exec <pluginName> [entryName] <alias> [-- args...]`.
//
// We treat the first positional after pluginName as the *entry* when the
// plugin has entries, otherwise as the alias. The generated PowerShell
// wrappers always pass the entry explicitly (or "" for the plugin's main
// command), so dispatch stays unambiguous.
// -----------------------------------------------------------------------------

type PluginExecCmd struct {
	Args []string `arg:"" name:"args" help:"<plugin> [entry] <alias> [args...]"`
}

func (c *PluginExecCmd) Run(e *env) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: onix plugin-exec <plugin> [entry] <alias> [args...]")
	}
	pluginName := c.Args[0]
	entryName := c.Args[1]
	rest := c.Args[2:]

	// Empty entryName means "the plugin's primary command" — we still
	// require positional alignment so the wrappers can pass "" without
	// shifting the rest of the argv.
	if entryName == "" {
		if len(rest) < 1 {
			return fmt.Errorf("usage: onix plugin-exec %s <alias> [args...]", pluginName)
		}
		// rest[0] is the alias; the rest are extras.
	} else {
		if len(rest) < 1 {
			return fmt.Errorf("usage: onix plugin-exec %s %s <alias> [args...]", pluginName, entryName)
		}
	}
	aliasName := rest[0]
	extras := rest[1:]
	if len(extras) > 0 && extras[0] == "--" {
		extras = extras[1:]
	}

	pf, err := LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	p := pf.FindPlugin(pluginName)
	if p == nil {
		return fmt.Errorf("unknown plugin %q (declared in %s)", pluginName, pluginsConfigPath(e.Home))
	}

	bin := pluginBinaryPath(e.Home, p.Repo)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("plugin binary missing: %s — run: onix plugin update %s", bin, pluginName)
	}

	target, err := resolveAliasPath(e, aliasName)
	if err != nil {
		return err
	}

	cmd := exec.Command(bin, extras...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Env-var contract matching the v1 plugins so existing plugin code
	// (onix-tts, onix-search, onix-find, onix-timer) keeps working.
	cmd.Env = append(os.Environ(),
		"ONIX_TARGET="+target,
		"ONIX_ALIAS="+strings.ToLower(aliasName),
		"ONIX_HOME="+e.Home,
		"ONIX_EDITOR="+resolveEditor(),
		"ONIX_MODULE="+p.Name,
		"ONIX_MODULE_CONFIG="+p.ConfigJSON(),
	)
	if entryName != "" {
		cmd.Env = append(cmd.Env, "ONIX_ENTRY="+entryName)
	}

	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("plugin %s: %w", pluginName, err)
	}
	return nil
}

