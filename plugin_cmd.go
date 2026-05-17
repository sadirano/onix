package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/plugins"
	"github.com/sadirano/onix/internal/snippet"
)

// PluginCmd is the umbrella for plugin subcommands. Each child is its
// own kong command struct so help and validation stay localised.
type PluginCmd struct {
	Add    PluginAddCmd    `cmd:"" help:"Install a new plugin from GitHub." examples:"onix plugin add sadirano/onix-tts --sha 123456"`
	Update PluginUpdateCmd `cmd:"" help:"Update installed plugins." examples:"onix plugin update tts"`
	Remove PluginRemoveCmd `cmd:"" aliases:"rm" help:"Uninstall a plugin." examples:"onix plugin rm tts"`
	List   PluginListCmd   `cmd:"" aliases:"ls" help:"List installed plugins." examples:"onix plugin ls"`
}

// PluginAddCmd installs a new plugin.
type PluginAddCmd struct {
	Repo     string `arg:"" help:"GitHub repo (user/repo)."`
	Name     string `help:"Local wrapper name (defaults to repo basename)." short:"n"`
	SHA      string `help:"Git SHA to pin to."`
	Unpinned bool   `help:"Don't enforce a SHA pin (tracks default branch)." short:"u"`
	Yes      bool   `help:"Skip confirmation prompt." short:"y"`
}

func (c *PluginAddCmd) Run(ctx context.Context, e *env) error {
	if strings.TrimSpace(c.SHA) == "" && !c.Unpinned {
		return fmt.Errorf("either --sha <hash> or --unpinned is required")
	}

	repo := plugins.NormalizeRepo(c.Repo)
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = plugins.DefaultWrapperName(repo)
	}

	// Load current state.
	pf, err := plugins.LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}

	// Clone or update.
	srcDir := plugins.SourceDir(e.Home, repo)
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

	// Resolve actual SHA, commit subject, and manifest entries.
	sha, err := gitHeadSHA(srcDir)
	if err != nil {
		return err
	}
	msg, _ := gitHeadMessage(srcDir)
	entries, err := readPluginManifest(srcDir)
	if err != nil {
		return err
	}

	// confirm. Removing any prior entry first lets a
	// re-add succeed without the validator complaining about itself.
	pf.Remove(name)
	candidate := plugins.Plugin{
		Name:    name,
		Repo:    repo,
		Entries: entries,
	}
	if c.Unpinned {
		candidate.Unpinned = true
		candidate.SHA = sha
	} else {
		candidate.SHA = sha
	}
	probe := &plugins.PluginsFile{Plugins: append(pf.Plugins, candidate)}
	if err := plugins.ValidatePlugins(probe, cfg.Actions); err != nil {
		return err
	}

	// Confirm with the user.
	if !c.Yes && !confirmInstall(repo, name, sha, msg, entries, c.Unpinned) {
		return fmt.Errorf("aborted by user")
	}

	// Build the binary.
	fmt.Printf("Building %s...\n", repo)
	if err := buildPlugin(srcDir, plugins.BinaryName(repo)); err != nil {
		return err
	}

	// Commit the change to plugins.toml and regenerate the snippet.
	pf.Plugins = append(pf.Plugins, candidate)
	if err := plugins.SavePlugins(e.Home, pf); err != nil {
		return err
	}
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}

	fmt.Printf("\nInstalled %s -> %s\n", name, plugins.BinaryPath(e.Home, repo))
	fmt.Println("Re-source $PROFILE (or restart PowerShell) to activate.")
	return nil
}

// PluginListCmd prints installed plugins.
type PluginListCmd struct{}

func (c *PluginListCmd) Run(ctx context.Context, e *env) error {
	pf, err := plugins.LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	if len(pf.Plugins) == 0 {
		if e.JSON {
			return printJSON([]string{})
		}
		fmt.Println("no plugins installed (run: onix plugin add <repo> --sha <hash>)")
		return nil
	}

	if e.JSON {
		return printJSON(pf.Plugins)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPO\tSHA\tSTATE")
	for _, p := range pf.Plugins {
		state := "installed"
		if _, err := os.Stat(plugins.BinaryPath(e.Home, p.Repo)); err != nil {
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

// PluginUpdateCmd bumps plugins to their latest commit or a specific SHA.
type PluginUpdateCmd struct {
	Name string `arg:"" optional:"" help:"Plugin name to update (defaults to all)."`
	SHA  string `name:"sha" help:"Bump the plugin's pin to a new SHA (requires Name)."`
	Yes  bool   `help:"Skip confirmation prompt." short:"y"`
}

func (c *PluginUpdateCmd) Run(ctx context.Context, e *env) error {
	pf, err := plugins.LoadPlugins(e.Home)
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

	cfg, err := config.LoadConfig(e.Home)
	if err != nil {
		return err
	}

	// Build the work list.
	var work []*plugins.Plugin
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
		srcDir := plugins.SourceDir(e.Home, p.Repo)
		if err := gitFetch(srcDir); err != nil {
			return fmt.Errorf("fetch %s: %w", p.Repo, err)
		}

		ref := p.SHA
		if c.SHA != "" {
			ref = c.SHA
		}
		if p.Unpinned && c.SHA == "" {
			ref = ""
		}
		if err := gitCheckout(srcDir, ref); err != nil {
			return err
		}

		newSHA, err := gitHeadSHA(srcDir)
		if err != nil {
			return err
		}

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

		if err := buildPlugin(srcDir, plugins.BinaryName(p.Repo)); err != nil {
			return err
		}
	}

	if err := plugins.ValidatePlugins(pf, cfg.Actions); err != nil {
		return err
	}
	if err := plugins.SavePlugins(e.Home, pf); err != nil {
		return err
	}
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}
	fmt.Println("Re-source $PROFILE (or restart PowerShell) if entries changed.")
	return nil
}

// PluginRemoveCmd uninstalls a plugin.
type PluginRemoveCmd struct {
	Name string `arg:"" help:"Plugin name."`
}

func (c *PluginRemoveCmd) Run(ctx context.Context, e *env) error {
	pf, err := plugins.LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	p := pf.FindPlugin(c.Name)
	if p == nil {
		return fmt.Errorf("unknown plugin %q", c.Name)
	}

	srcDir := plugins.SourceDir(e.Home, p.Repo)
	if err := os.RemoveAll(srcDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", srcDir, err)
	}
	pf.Remove(c.Name)
	if err := plugins.SavePlugins(e.Home, pf); err != nil {
		return err
	}
	if err := snippet.RegenerateShellSnippet(e.Home); err != nil {
		return err
	}
	fmt.Printf("Removed %s\n", c.Name)
	return nil
}

// PluginExecCmd runs a plugin.
type PluginExecCmd struct {
	Args []string `arg:"" help:"Plugin name and arguments."`
}

func (c *PluginExecCmd) Run(ctx context.Context, e *env) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: onix plugin-exec <plugin> [entry] <alias> [args...]")
	}
	pluginName := c.Args[0]
	entryName := c.Args[1]
	rest := c.Args[2:]

	if entryName == "" {
		if len(rest) < 1 {
			return fmt.Errorf("usage: onix plugin-exec %s <alias> [args...]", pluginName)
		}
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

	pf, err := plugins.LoadPlugins(e.Home)
	if err != nil {
		return err
	}
	p := pf.FindPlugin(pluginName)
	if p == nil {
		return fmt.Errorf("unknown plugin %q (declared in %s)", pluginName, plugins.ConfigPath(e.Home))
	}

	bin := plugins.BinaryPath(e.Home, p.Repo)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("plugin binary missing: %s — run: onix plugin update %s", bin, pluginName)
	}

	target, err := resolveAliasPath(e, aliasName)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, bin, extras...)
	cmd.Dir = target
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = append(
		os.Environ(),
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
