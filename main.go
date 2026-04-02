package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sadirano/onix/internal/alias"
	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/dispatch"
	"github.com/sadirano/onix/internal/installer"
)

// Onix — a modular directory navigator.
//
// Direct invocation:
//
//	onix                          open alias file in editor
//	onix -a <alias> -d <path>     register an alias
//	onix <alias>                  open cmd.exe in target directory (built-in default)
//	onix install [name]           install one or all modules
//	onix add <user/repo>          declare a new module in config
//	onix remove <name>            remove a module
//	onix update [name]            update one or all modules
//	onix list                     list declared modules
//	onix init                     set up ~/.onix/ directory structure
//
// Module dispatch (via wrapper):
//
//	ONIX_MODULE=sg onix <alias> [args...]

func main() {
	t := newTimer()
	defer t.report()

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}

	// Module dispatch — invoked via a .cmd wrapper that sets ONIX_MODULE.
	if mod := strings.TrimSpace(os.Getenv("ONIX_MODULE")); mod != "" {
		t.mark("config loaded")
		args := os.Args[1:]
		if len(args) == 0 {
			fatal("usage: %s <alias> [args...]", mod)
		}
		aliasName := args[0]
		if err := dispatch.Run(mod, aliasName, args[1:], cfg); err != nil {
			fatal("%v", err)
		}
		t.mark("dispatch")
		return
	}

	args := os.Args[1:]

	// No args — open the alias file in the editor.
	if len(args) == 0 {
		openAliasFile(cfg)
		return
	}

	// Management commands.
	switch args[0] {
	case "install":
		t.mark("config loaded")
		if len(args) > 1 {
			if err := installer.Install(args[1], cfg); err != nil {
				fatal("%v", err)
			}
		} else {
			if err := installer.InstallAll(cfg); err != nil {
				fatal("%v", err)
			}
		}
		t.mark("install")
		return

	case "add":
		if len(args) < 2 {
			fatal("usage: onix add <user/repo>")
		}
		if err := installer.Add(args[1], cfg); err != nil {
			fatal("%v", err)
		}
		return

	case "remove":
		if len(args) < 2 {
			fatal("usage: onix remove <name>")
		}
		if err := installer.Remove(args[1], cfg); err != nil {
			fatal("%v", err)
		}
		return

	case "update":
		t.mark("config loaded")
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		if err := installer.Update(name, cfg); err != nil {
			fatal("%v", err)
		}
		t.mark("update")
		return

	case "list":
		installer.List(cfg)
		return

	case "init":
		if err := installer.Init(); err != nil {
			fatal("%v", err)
		}
		return

	case "-h", "--help", "help":
		printHelp()
		return
	}

	// Alias registration: onix -a <alias> -d <path>
	if args[0] == "-a" || args[0] == "--alias" {
		registerAlias(args)
		return
	}

	// Default: resolve alias and open an interactive shell there.
	t.mark("config loaded")
	aliasName := args[0]
	debug := cfg.Settings.Debug || os.Getenv("ONIX_DEBUG") == "1" || os.Getenv("OMNI_DEBUG") == "1"

	target, err := alias.Resolve(aliasName, debug)
	if err != nil {
		fatal("%v", err)
	}
	t.mark("alias resolved")

	if err := os.MkdirAll(target, 0o755); err != nil {
		fatal("create target: %v", err)
	}
	if err := os.Chdir(target); err != nil {
		fatal("chdir: %v", err)
	}
	t.mark("chdir")

	cmd := exec.Command("cmd.exe", "/K")
	cmd.Dir = target
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		fatal("open shell: %v", err)
	}
	t.mark("shell spawned")
	_ = cmd.Wait()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func registerAlias(args []string) {
	var aliasName, destination, subdir string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-a", "--alias":
			if i+1 < len(args) {
				aliasName = args[i+1]
				i++
			}
		case "-d", "--destination":
			if i+1 < len(args) {
				destination = args[i+1]
				i++
			}
		case "-s", "--subdir":
			if i+1 < len(args) {
				subdir = args[i+1]
				i++
			}
		}
	}

	if aliasName == "" || destination == "" {
		fatal("usage: onix -a <alias> -d <destination>")
	}

	if subdir != "" {
		destination = filepath.Join(destination, subdir)
	}

	if err := alias.Register(aliasName, destination); err != nil {
		fatal("register alias: %v", err)
	}
	fmt.Printf("Registered %q -> %q\n", aliasName, destination)
}

func openAliasFile(cfg *config.Config) {
	editor := cfg.Settings.Editor
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("OMNI_EDITOR"))
	}
	if editor == "" {
		editor = "nvim"
	}

	f := alias.FilePath()
	cmd := exec.Command(editor, f)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		fatal("open editor: %v", err)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "onix: "+format+"\n", a...)
	os.Exit(1)
}

func printHelp() {
	fmt.Print(`Onix — modular directory navigator

Usage:
  onix                          open alias file in editor
  onix <alias>                  open shell in target directory
  onix -a <alias> -d <path>     register an alias
  onix install [name]           install one or all modules
  onix add <user/repo>          declare a module in config
  onix remove <name>            remove a module
  onix update [name]            update one or all modules
  onix list                     list declared modules
  onix init                     initialise ~/.onix/ structure
  onix help                     show this message

Module invocation (via generated wrappers):
  <module> <alias> [args...]    e.g. sg myproject foo bar

Environment:
  ONIX_MODULE        set by .cmd wrappers to select the module
  ONIX_DEBUG=1       verbose trace
  ONIX_TIMING=1      print phase timings to stderr
  ONIX_ENV           override alias file path
  EDITOR             preferred editor (default: nvim)

Config:  ~/.onix/config.toml
Modules: ~/.onix/modules/
Bin:     ~/.onix/bin/   ← add this to PATH
`)
}

// ---------------------------------------------------------------------------
// Checkpoint timer — activated by ONIX_TIMING=1
// ---------------------------------------------------------------------------

type checkpoint struct {
	name    string
	elapsed time.Duration
	delta   time.Duration
}

type timer struct {
	enabled     bool
	start       time.Time
	last        time.Time
	checkpoints []checkpoint
}

func newTimer() *timer {
	t := &timer{
		enabled: os.Getenv("ONIX_TIMING") == "1",
		start:   time.Now(),
	}
	t.last = t.start
	return t
}

func (t *timer) mark(name string) {
	if !t.enabled {
		return
	}
	now := time.Now()
	t.checkpoints = append(t.checkpoints, checkpoint{
		name:    name,
		elapsed: now.Sub(t.start),
		delta:   now.Sub(t.last),
	})
	t.last = now
}

func (t *timer) report() {
	if !t.enabled || len(t.checkpoints) == 0 {
		return
	}
	total := time.Since(t.start)
	fmt.Fprintln(os.Stderr, "\n[ONIX TIMING] ----------------------------------------")
	fmt.Fprintf(os.Stderr, "  %-28s  %12s  %12s\n", "phase", "delta", "elapsed")
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("-", 56))
	for _, cp := range t.checkpoints {
		fmt.Fprintf(os.Stderr, "  %-28s  %12s  %12s\n", cp.name, fmtDur(cp.delta), fmtDur(cp.elapsed))
	}
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("-", 56))
	fmt.Fprintf(os.Stderr, "  %-28s  %12s\n", "TOTAL", fmtDur(total))
	fmt.Fprintln(os.Stderr, "[ONIX TIMING] ----------------------------------------")
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.3fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
}
