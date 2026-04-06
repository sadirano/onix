package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	rdebug "runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
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

// shortcuts maps executable basenames to their implicit action flag.
// Executables in ~/.onix/bin/ are copies of onix.exe named after each
// shortcut; the binary detects its own name and injects the flag.
var shortcuts = map[string]string{
	"o":  "",
	"c":  "",
	"s":  "-e",
	"n":  "-n",
	"y":  "-y",
	"f":  "-f",
	"r":  "-r",
	"sg": "-sg",
	"ff": "-ff",
}

var buildVersion = "dev"
var visuals = defaultVisualConfig()

type visualConfig struct {
	FZF visualFZFConfig `toml:"fzf"`
	RG  rgConfig        `toml:"rg"`
}

type rgConfig struct {
	Color string `toml:"color"`
	Case  string `toml:"case"`
}

type visualFZFConfig struct {
	Destination visualPickerConfig `toml:"destination"`
	SG          visualPickerConfig `toml:"sg"`
	FF          visualPickerConfig `toml:"ff"`
}

type visualPickerConfig struct {
	Prompt        string `toml:"prompt"`
	Layout        string `toml:"layout"`
	Color         string `toml:"color"`
	Preview       string `toml:"preview"`
	PreviewWindow string `toml:"preview_window"`
	Header        string `toml:"header"`
	Height        string `toml:"height"`
}

const visualConfigFileName = "onix.visual.toml"

const visualConfigStarter = `# onix.visual.toml

[rg]
# color controls rg's --color flag (always, never, auto)
color = "always"
# case controls case sensitivity: smart, sensitive, insensitive
case = "smart"

[fzf.destination]
prompt = "Destination > "
layout = "reverse-list"
preview = "dir /b \"{}\" 2>nul"
preview_window = "right:40%,border-left"
header = "Enter to confirm  |  Esc to type manually"
height = "60%"

[fzf.sg]
prompt = "> "
layout = "default"
color = "hl:-1:underline,hl+:-1:underline:reverse"
preview = "bat --color=always {1} --highlight-line {2}"
preview_window = "up,60%,border-bottom,+{2}+3/3,~3"

[fzf.ff]
prompt = "> "
layout = "default"
`

func main() {
	invokedAs := execBasename()

	// Named-shortcut dispatch: if invoked as s.exe, n.exe, etc., inject the
	// corresponding action flag before any other argument processing.
	if flag, ok := shortcuts[invokedAs]; ok && flag != "" {
		os.Args = append(os.Args, flag)
	}

	t := newTimer()
	defer t.report()

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}
	applyAliasFileOverride(cfg)
	debugEnabled := isDebugEnabled(cfg)
	initVisualConfig(debugEnabled)
	if debugEnabled {
		printBuildDebugInfo()
	}

	args := os.Args[1:]

	// Module dispatch — invoked via a .cmd wrapper that sets ONIX_MODULE.
	if mod := strings.TrimSpace(os.Getenv("ONIX_MODULE")); mod != "" && !isCoreCommandInvocation(args) {
		t.mark("config loaded")
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

	case "shortcuts":
		if err := installer.InstallShortcuts(); err != nil {
			fatal("%v", err)
		}
		return

	case "theme", "themes":
		if err := handleThemeCommand(args[1:], debugEnabled); err != nil {
			fatal("%v", err)
		}
		return

	case "-h", "--help", "help":
		printHelp()
		return
	}

	// Alias registration: onix -a <alias> -d <path>
	if args[0] == "-a" || args[0] == "--alias" {
		destination := registerAlias(args)
		if invokedAs == "o" || invokedAs == "c" {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				fatal("create target: %v", err)
			}
			cmd := exec.Command("cmd.exe", "/K")
			cmd.Dir = destination
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			t.mark("shell spawned")
			if err := cmd.Start(); err != nil {
				fatal("open shell: %v", err)
			}
			_ = cmd.Wait()
		}
		return
	}

	// Default: resolve alias and perform action.
	t.mark("config loaded")
	aliasName := args[0]
	debug := debugEnabled

	// Parse action flags and subdir from remaining args.
	// The .cmd wrappers append the flag last (e.g. `o %* -e`), so positional
	// extras (filenames, commands) appear before the flag — collect them separately.
	var action, subdir string
	var extras []string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-e":
			action = "e"
		case "-n":
			action = "n"
		case "-y":
			action = "y"
		case "-f":
			action = "f"
		case "-r":
			action = "r"
		case "-sg":
			action = "sg"
		case "-ff":
			action = "ff"
		case "-s", "--subdir":
			if i+1 < len(args) {
				subdir = args[i+1]
				i++
			}
		default:
			extras = append(extras, args[i])
		}
	}

	target, err := alias.Resolve(aliasName, debug)
	if err != nil {
		// Unknown alias — let the user pick a destination interactively.
		dest := selectDestination(aliasName)
		if dest == "" {
			fatal("no destination provided")
		}
		if err := alias.Register(aliasName, dest); err != nil {
			fatal("register alias: %v", err)
		}
		fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, dest)
		target = dest
	}
	t.mark("alias resolved")

	if subdir != "" {
		target = filepath.Join(target, subdir)
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		fatal("create target: %v", err)
	}
	if err := os.Chdir(target); err != nil {
		fatal("chdir: %v", err)
	}
	t.mark("chdir")

	switch action {
	case "e":
		cmd := exec.Command("cmd.exe", "/C", "start", "explorer.exe", target)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			fatal("open explorer: %v", err)
		}

	case "n":
		cmd := exec.Command(resolveEditor(cfg), ".")
		cmd.Dir = target
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			fatal("open editor: %v", err)
		}

	case "y":
		fmt.Println(target)

	case "f":
		fArgs := extras
		if len(fArgs) == 0 {
			fArgs = []string{"."}
		}
		cmd := exec.Command(resolveEditor(cfg), fArgs...)
		cmd.Dir = target
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			fatal("open editor: %v", err)
		}

	case "r":
		if len(extras) == 0 {
			fatal("usage: onix <alias> -r \"<command>\"")
		}
		cmd := exec.Command("cmd.exe", "/C", strings.Join(extras, " "))
		cmd.Dir = target
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ProcessState != nil {
				os.Exit(exitErr.ProcessState.ExitCode())
			}
			fatal("run command: %v", err)
		}

	case "sg":
		if err := runSG(target, extras, cfg); err != nil {
			fatal("%v", err)
		}

	case "ff":
		if err := runFF(target, extras, cfg); err != nil {
			fatal("%v", err)
		}

	default:
		cmd := exec.Command("cmd.exe", "/K")
		cmd.Dir = target
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		t.mark("shell spawned")
		if err := cmd.Start(); err != nil {
			fatal("open shell: %v", err)
		}
		_ = cmd.Wait()
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func registerAlias(args []string) string {
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
	fmt.Printf("Registered \"%s\" -> \"%s\"\n", aliasName, destination)
	return destination
}

func execBasename() string {
	base := filepath.Base(os.Args[0])
	return strings.ToLower(strings.TrimSuffix(base, ".exe"))
}

func resolveEditor(cfg *config.Config) string {
	if e := cfg.Settings.Editor; e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("OMNI_EDITOR")); e != "" {
		return e
	}
	return "nvim"
}

func isDebugEnabled(cfg *config.Config) bool {
	return cfg.Settings.Debug || os.Getenv("ONIX_DEBUG") == "1" || os.Getenv("OMNI_DEBUG") == "1"
}

func printBuildDebugInfo() {
	onixPath, infoErr := resolveOnixBinaryInfo()
	version := resolvedBuildVersion()
	modifiedAt := "unknown"
	if infoErr == nil {
		if fi, err := os.Stat(onixPath); err == nil {
			modifiedAt = fi.ModTime().Format(time.RFC3339)
		}
	}
	if onixPath == "" {
		onixPath = "<unknown>"
	}
	fmt.Fprintf(os.Stderr, "[ONIX] build_version=%s onix_exe=%s modified_at=%s\n", version, onixPath, modifiedAt)
}

func resolvedBuildVersion() string {
	if v := strings.TrimSpace(buildVersion); v != "" && v != "dev" {
		return v
	}
	if bi, ok := rdebug.ReadBuildInfo(); ok && bi != nil {
		if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return bi.Main.Version
		}
		for _, setting := range bi.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				if len(setting.Value) > 12 {
					return setting.Value[:12]
				}
				return setting.Value
			}
		}
	}
	return "dev"
}

func resolveOnixBinaryInfo() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe = strings.TrimSpace(exe)
	if strings.EqualFold(filepath.Base(exe), "onix.exe") {
		return exe, nil
	}
	defaultOnix := filepath.Join(config.Dir(), "onix.exe")
	if _, err := os.Stat(defaultOnix); err == nil {
		return defaultOnix, nil
	}
	return exe, nil
}

func defaultVisualConfig() visualConfig {
	return visualConfig{
		RG: rgConfig{
			Color: "always",
			Case:  "smart",
		},
		FZF: visualFZFConfig{
			Destination: visualPickerConfig{
				Prompt:        "Destination > ",
				Layout:        "reverse-list",
				Preview:       `dir /b "{}" 2>nul`,
				PreviewWindow: "right:40%,border-left",
				Header:        "Enter to confirm  |  Esc to type manually",
				Height:        "60%",
			},
			SG: visualPickerConfig{
				Prompt:        "> ",
				Layout:        "default",
				Color:         "hl:-1:underline,hl+:-1:underline:reverse",
				Preview:       `bat --color=always {1} --highlight-line {2}`,
				PreviewWindow: "up,60%,border-bottom,+{2}+3/3,~3",
			},
			FF: visualPickerConfig{
				Prompt: "> ",
				Layout: "default",
			},
		},
	}
}

func initVisualConfig(debug bool) {
	cfg, path, err := loadVisualConfig()
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "[ONIX] visual_config_error=%v\n", err)
		}
		return
	}
	visuals = cfg
	if debug && path != "" {
		fmt.Fprintf(os.Stderr, "[ONIX] visual_config=%s\n", path)
	}
}

func loadVisualConfig() (visualConfig, string, error) {
	cfg := defaultVisualConfig()
	onixPath, err := resolveOnixBinaryInfo()
	if err != nil || strings.TrimSpace(onixPath) == "" {
		return cfg, "", err
	}

	configPath := filepath.Join(filepath.Dir(onixPath), visualConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if writeErr := os.WriteFile(configPath, []byte(visualConfigStarter), 0o644); writeErr != nil {
			return cfg, configPath, fmt.Errorf("create %s: %w", configPath, writeErr)
		}
		return cfg, configPath, nil
	} else if err != nil {
		return cfg, configPath, err
	}

	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return defaultVisualConfig(), configPath, fmt.Errorf("decode %s: %w", configPath, err)
	}
	cfg.applyDefaults()
	return cfg, configPath, nil
}

func (v *visualConfig) applyDefaults() {
	def := defaultVisualConfig()
	v.RG.Color = fallbackString(v.RG.Color, def.RG.Color)
	v.RG.Case = fallbackString(v.RG.Case, def.RG.Case)

	v.FZF.Destination.Prompt = fallbackString(v.FZF.Destination.Prompt, def.FZF.Destination.Prompt)
	v.FZF.Destination.Layout = fallbackString(v.FZF.Destination.Layout, def.FZF.Destination.Layout)
	v.FZF.Destination.Preview = fallbackString(v.FZF.Destination.Preview, def.FZF.Destination.Preview)
	v.FZF.Destination.PreviewWindow = fallbackString(v.FZF.Destination.PreviewWindow, def.FZF.Destination.PreviewWindow)
	v.FZF.Destination.Header = fallbackString(v.FZF.Destination.Header, def.FZF.Destination.Header)
	v.FZF.Destination.Height = fallbackString(v.FZF.Destination.Height, def.FZF.Destination.Height)

	v.FZF.SG.Prompt = fallbackString(v.FZF.SG.Prompt, def.FZF.SG.Prompt)
	v.FZF.SG.Layout = fallbackString(v.FZF.SG.Layout, def.FZF.SG.Layout)
	v.FZF.SG.Color = fallbackString(v.FZF.SG.Color, def.FZF.SG.Color)
	v.FZF.SG.Preview = fallbackString(v.FZF.SG.Preview, def.FZF.SG.Preview)
	v.FZF.SG.PreviewWindow = fallbackString(v.FZF.SG.PreviewWindow, def.FZF.SG.PreviewWindow)

	v.FZF.FF.Prompt = fallbackString(v.FZF.FF.Prompt, def.FZF.FF.Prompt)
	v.FZF.FF.Layout = fallbackString(v.FZF.FF.Layout, def.FZF.FF.Layout)
	v.FZF.FF.Preview = fallbackString(v.FZF.FF.Preview, def.FZF.FF.Preview)
	v.FZF.FF.PreviewWindow = fallbackString(v.FZF.FF.PreviewWindow, def.FZF.FF.PreviewWindow)
}

func fallbackString(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

func isCoreCommandInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "install", "add", "remove", "update", "list", "init", "shortcuts",
		"theme", "themes", "help", "-h", "--help", "-a", "--alias":
		return true
	default:
		return false
	}
}

func handleThemeCommand(args []string, debug bool) error {
	onixPath, err := resolveOnixBinaryInfo()
	if err != nil {
		return fmt.Errorf("resolve onix path: %w", err)
	}
	themeDir := filepath.Dir(onixPath)
	activePath := filepath.Join(themeDir, visualConfigFileName)
	if err := ensureVisualConfigFile(activePath); err != nil {
		return err
	}

	themes, err := listVisualThemes(themeDir)
	if err != nil {
		return err
	}
	if len(themes) == 0 {
		return fmt.Errorf("no theme files found in %s", themeDir)
	}

	if len(args) > 0 {
		if strings.EqualFold(args[0], "list") || strings.EqualFold(args[0], "ls") {
			current := detectActiveTheme(activePath, themes)
			for _, t := range themes {
				tag := ""
				if t == current {
					tag = " (current)"
				}
				fmt.Printf("%s%s\n", filepath.Base(t), tag)
			}
			return nil
		}
		selected, err := resolveThemeArg(strings.TrimSpace(args[0]), themes)
		if err != nil {
			return err
		}
		return applyVisualTheme(selected, activePath, debug)
	}

	selected, err := pickThemeInteractive(themes, activePath)
	if err != nil {
		return err
	}
	if selected == "" {
		return nil
	}
	return applyVisualTheme(selected, activePath, debug)
}

func ensureVisualConfigFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(visualConfigStarter), 0o644); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

func listVisualThemes(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "onix.visual.*.toml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func resolveThemeArg(input string, themes []string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("theme name is required")
	}
	lower := strings.ToLower(input)
	for _, t := range themes {
		base := strings.ToLower(filepath.Base(t))
		if lower == base || lower == strings.TrimSuffix(base, ".toml") {
			return t, nil
		}
	}
	return "", fmt.Errorf("theme %q not found", input)
}

func pickThemeInteractive(themes []string, activePath string) (string, error) {
	current := detectActiveTheme(activePath, themes)
	if _, err := exec.LookPath("fzf"); err == nil {
		return pickThemeWithFZF(themes, current)
	}
	return pickThemeWithPrompt(themes, current)
}

func detectActiveTheme(activePath string, themes []string) string {
	activeData, err := os.ReadFile(activePath)
	if err != nil {
		return ""
	}
	for _, t := range themes {
		data, err := os.ReadFile(t)
		if err != nil {
			continue
		}
		if bytes.Equal(activeData, data) {
			return t
		}
	}
	return ""
}

func pickThemeWithFZF(themes []string, current string) (string, error) {
	var input bytes.Buffer
	for _, t := range themes {
		label := filepath.Base(t)
		if t == current {
			label += " [current]"
		}
		input.WriteString(label)
		input.WriteByte('\t')
		input.WriteString(t)
		input.WriteByte('\n')
	}

	cmd := exec.Command("fzf",
		"--delimiter", "\t",
		"--with-nth", "1",
		"--prompt", "Theme > ",
		"--preview", `cmd /C type {2}`,
		"--preview-window", "right:60%,border-left",
	)
	cmd.Stdin = &input
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", fmt.Errorf("run fzf: %w", err)
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", nil
	}
	parts := strings.SplitN(line, "\t", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid fzf selection")
	}
	return strings.TrimSpace(parts[1]), nil
}

func pickThemeWithPrompt(themes []string, current string) (string, error) {
	fmt.Println("Select a theme:")
	for i, t := range themes {
		tag := ""
		if t == current {
			tag = " (current)"
		}
		fmt.Printf("  %d) %s%s\n", i+1, filepath.Base(t), tag)
	}
	fmt.Print("Theme number (blank to cancel): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(themes) {
		return "", fmt.Errorf("invalid selection")
	}
	return themes[idx-1], nil
}

func applyVisualTheme(themePath, activePath string, debug bool) error {
	data, err := os.ReadFile(themePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(activePath, data, 0o644); err != nil {
		return err
	}
	if debug {
		fmt.Fprintf(os.Stderr, "[ONIX] theme_applied=%s\n", themePath)
	}
	fmt.Printf("Applied %s\n", filepath.Base(themePath))
	return nil
}

func applyAliasFileOverride(cfg *config.Config) {
	aliasPath := strings.TrimSpace(cfg.Settings.AliasFile)
	if aliasPath == "" {
		return
	}
	for _, env := range []string{"ONIX_ENV", "ONIX_ALIAS_FILE", "OMNI_ENV", "OMNI_ALIAS_FILE"} {
		if strings.TrimSpace(os.Getenv(env)) != "" {
			return
		}
	}
	_ = os.Setenv("ONIX_ALIAS_FILE", aliasPath)
}

func openAliasFile(cfg *config.Config) {
	f := alias.FilePath()
	cmd := exec.Command(resolveEditor(cfg), f)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		fatal("open editor: %v", err)
	}
	_ = cmd.Wait()
}

// selectDestination opens an fzf directory picker seeded with aliasName as
// the initial query. Falls back to a plain text prompt if fzf is unavailable
// or the user presses Esc.
func selectDestination(aliasName string) string {
	if selected := fzfPickDir(aliasName); selected != "" {
		return selected
	}
	return promptDestination(aliasName)
}

func fzfPickDir(query string) string {
	if _, err := exec.LookPath("fzf"); err != nil {
		return ""
	}

	var input *bytes.Buffer

	// Prefer es (Everything) — results are instant.
	if _, err := exec.LookPath("es"); err == nil {
		if out, err := exec.Command("es", "-ad", query).Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
			input = bytes.NewBuffer(out)
		}
	}

	// Fallback: walk drive roots up to 3 levels deep.
	if input == nil {
		var buf bytes.Buffer
		for _, drive := range availableDrives() {
			filepath.Walk(drive, func(path string, info os.FileInfo, err error) error {
				if err != nil || !info.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(drive, path)
				if rel == "." {
					return nil
				}
				if strings.Count(rel, string(filepath.Separator)) >= 3 {
					return filepath.SkipDir
				}
				buf.WriteString(path + "\n")
				return nil
			})
		}
		input = &buf
	}

	fzfArgs := []string{
		"--prompt", visuals.FZF.Destination.Prompt,
		"--query", query,
		"--height", "100%",
	}
	fzfArgs = appendLayoutArg(fzfArgs, visuals.FZF.Destination.Layout)
	if header := strings.TrimSpace(visuals.FZF.Destination.Header); header != "" {
		fzfArgs = append(fzfArgs, "--header", header)
	}
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = input
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		return "" // Esc pressed or fzf error
	}
	return strings.TrimSpace(string(out))
}

func availableDrives() []string {
	var drives []string
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + `:\`
		if _, err := os.Stat(drive); err == nil {
			drives = append(drives, drive)
		}
	}
	return drives
}

func promptDestination(aliasName string) string {
	fmt.Printf("Destination for %q: ", aliasName)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

type searchMatch struct {
	Path string
	Line int
	Col  int
	Text string
}

func runSG(target string, extras []string, cfg *config.Config) error {
	if len(extras) == 0 {
		return fmt.Errorf("usage: sg <alias> <query>")
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return fmt.Errorf("sg requires ripgrep (rg) in PATH")
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("sg requires fzf in PATH")
	}

	query := strings.TrimSpace(strings.Join(extras, " "))
	rgColor := "--color=" + visuals.RG.Color
	rgCase := rgCaseFlag(visuals.RG.Case)
	rgArgs := []string{"--vimgrep", rgCase, rgColor, query, "."}
	rgCmd := exec.Command("rg", rgArgs...)
	rgCmd.Dir = target
	rgOut, err := rgCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			fmt.Println("No matches found.")
			return nil
		}
		return fmt.Errorf("run rg: %w", err)
	}

	preview := resolvePreviewCommand(visuals.FZF.SG.Preview, `cmd /C type {1}`)

	fzfArgs := []string{
		"--ansi",
		"--multi",
		"--delimiter", ":",
		"--with-nth", "1,2,4..",
		"--prompt", visuals.FZF.SG.Prompt,
		"--query", query,
		"--preview", preview,
		"--preview-window", visuals.FZF.SG.PreviewWindow,
	}
	fzfArgs = appendLayoutArg(fzfArgs, visuals.FZF.SG.Layout)
	if color := strings.TrimSpace(visuals.FZF.SG.Color); color != "" {
		fzfArgs = append(fzfArgs, "--color", color)
	}
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Dir = target
	fzfCmd.Stdin = bytes.NewReader(rgOut)
	fzfCmd.Stderr = os.Stderr
	selectedRaw, err := fzfCmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("run fzf: %w", err)
	}

	lines := splitNonEmptyLines(selectedRaw)
	if len(lines) == 0 {
		return nil
	}

	matches := make([]searchMatch, 0, len(lines))
	for _, line := range lines {
		m, ok := parseVimgrepLine(line)
		if ok {
			matches = append(matches, m)
		}
	}
	if len(matches) == 0 {
		return nil
	}

	return openSearchMatches(resolveEditor(cfg), target, matches)
}

func runFF(target string, extras []string, cfg *config.Config) error {
	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("ff requires fzf in PATH")
	}

	query := strings.TrimSpace(strings.Join(extras, " "))
	if _, err := exec.LookPath("es"); err == nil {
		return runFFWithEverythingStream(target, query, cfg)
	}
	return runFFWithWalkFallback(target, query, cfg)
}

func runFFWithEverythingStream(target, query string, cfg *config.Config) error {
	esArgs := []string{"-p", "-path", target}
	if query != "" {
		esArgs = append(esArgs, query)
	}
	esCmd := exec.Command("es", esArgs...)
	esCmd.Stderr = os.Stderr
	esOut, err := esCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("run es: %w", err)
	}

	preview := resolvePreviewCommand(visuals.FZF.FF.Preview, "")
	fzfArgs := []string{
		"--multi",
		"--expect=ctrl-e",
		"--prompt", visuals.FZF.FF.Prompt,
		"--query", query,
	}
	fzfArgs = appendLayoutArg(fzfArgs, visuals.FZF.FF.Layout)
	if preview != "" {
		fzfArgs = append(fzfArgs, "--preview", preview)
		if window := strings.TrimSpace(visuals.FZF.FF.PreviewWindow); window != "" {
			fzfArgs = append(fzfArgs, "--preview-window", window)
		}
	}
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = esOut
	fzfCmd.Stderr = os.Stderr

	if err := esCmd.Start(); err != nil {
		return fmt.Errorf("run es: %w", err)
	}
	out, err := fzfCmd.Output()
	_ = esCmd.Wait()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("run fzf: %w", err)
	}
	key, selected := parseFzfExpectOutput(out)
	if len(selected) == 0 {
		return nil
	}
	if key == "ctrl-e" {
		for _, file := range selected {
			if err := openInExplorer(file); err != nil {
				return err
			}
		}
		return nil
	}

	return openFilesInEditor(resolveEditor(cfg), selected)
}

func runFFWithWalkFallback(target, query string, cfg *config.Config) error {
	files, err := gatherFilesWithWalk(target, query)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println("No files found.")
		return nil
	}

	var input bytes.Buffer
	for _, file := range files {
		input.WriteString(file)
		input.WriteByte('\n')
	}

	preview := resolvePreviewCommand(visuals.FZF.FF.Preview, "")
	fzfArgs := []string{
		"--multi",
		"--expect=ctrl-e",
		"--prompt", visuals.FZF.FF.Prompt,
		"--query", query,
	}
	fzfArgs = appendLayoutArg(fzfArgs, visuals.FZF.FF.Layout)
	if preview != "" {
		fzfArgs = append(fzfArgs, "--preview", preview)
		if window := strings.TrimSpace(visuals.FZF.FF.PreviewWindow); window != "" {
			fzfArgs = append(fzfArgs, "--preview-window", window)
		}
	}
	fzfCmd := exec.Command("fzf", fzfArgs...)
	fzfCmd.Stdin = &input
	fzfCmd.Stderr = os.Stderr
	out, err := fzfCmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("run fzf: %w", err)
	}

	key, selected := parseFzfExpectOutput(out)
	if len(selected) == 0 {
		return nil
	}
	if key == "ctrl-e" {
		for _, file := range selected {
			if err := openInExplorer(file); err != nil {
				return err
			}
		}
		return nil
	}

	return openFilesInEditor(resolveEditor(cfg), selected)
}

func gatherFilesWithWalk(root, query string) ([]string, error) {
	queryLower := strings.ToLower(query)
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if queryLower != "" && !strings.Contains(strings.ToLower(filepath.Base(path)), queryLower) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func rgCaseFlag(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "sensitive":
		return "--case-sensitive"
	case "insensitive":
		return "--ignore-case"
	default:
		return "--smart-case"
	}
}

func parseVimgrepLine(line string) (searchMatch, bool) {
	parts := strings.SplitN(line, ":", 4)
	if len(parts) < 4 {
		return searchMatch{}, false
	}
	ln, err := strconv.Atoi(parts[1])
	if err != nil {
		return searchMatch{}, false
	}
	col, err := strconv.Atoi(parts[2])
	if err != nil {
		col = 1
	}
	return searchMatch{
		Path: parts[0],
		Line: ln,
		Col:  col,
		Text: parts[3],
	}, true
}

func splitNonEmptyLines(b []byte) []string {
	raw := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func parseFzfExpectOutput(out []byte) (string, []string) {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "", nil
	}

	key := strings.TrimSpace(lines[0])
	var selected []string
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		selected = append(selected, line)
	}
	return key, selected
}

func resolvePreviewCommand(configured, fallback string) string {
	preview := strings.TrimSpace(configured)
	if preview == "" {
		return fallback
	}
	token := firstCommandToken(preview)
	if token == "" {
		return fallback
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(token), filepath.Ext(token)))
	if base == "bat" {
		if _, err := exec.LookPath("bat"); err != nil {
			return fallback
		}
	}
	return preview
}

func appendLayoutArg(args []string, layout string) []string {
	layout = strings.TrimSpace(layout)
	if layout == "" || strings.EqualFold(layout, "default") {
		return args
	}
	return append(args, "--layout", layout)
}

func firstCommandToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] == '"' {
		rest := command[1:]
		if idx := strings.Index(rest, `"`); idx >= 0 {
			return rest[:idx]
		}
		return strings.Trim(rest, `"`)
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func openSearchMatches(editor, dir string, matches []searchMatch) error {
	base := editorBase(editor)
	if base == "code" || base == "code-insiders" {
		args := make([]string, 0, len(matches)*2)
		for _, m := range matches {
			args = append(args, "-g", fmt.Sprintf("%s:%d:%d", m.Path, m.Line, m.Col))
		}
		return runEditorCommand(editor, dir, args...)
	}
	if base == "nvim" || base == "vim" {
		if len(matches) == 1 {
			return runEditorCommand(editor, dir, fmt.Sprintf("+%d", matches[0].Line), matches[0].Path)
		}
		tmp, err := os.CreateTemp("", "onix-sg-*.qf")
		if err != nil {
			return fmt.Errorf("create temp quickfix file: %w", err)
		}
		defer os.Remove(tmp.Name())
		for _, m := range matches {
			fmt.Fprintf(tmp, "%s:%d:%d:%s\n", m.Path, m.Line, m.Col, m.Text)
		}
		_ = tmp.Close()
		return runEditorCommand(editor, dir, "-q", tmp.Name())
	}
	paths := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		if _, ok := seen[m.Path]; ok {
			continue
		}
		seen[m.Path] = struct{}{}
		paths = append(paths, m.Path)
	}
	return runEditorCommand(editor, dir, paths...)
}

func openFilesInEditor(editor string, files []string) error {
	return runEditorCommand(editor, "", files...)
}

func runEditorCommand(editor, dir string, args ...string) error {
	cmd := exec.Command(editor, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open editor: %w", err)
	}
	return nil
}

func openInExplorer(path string) error {
	cmd := exec.Command("cmd.exe", "/C", "start", "explorer.exe", fmt.Sprintf(`/select,%s`, path))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func editorBase(editor string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(editor)))
	return strings.TrimSuffix(base, filepath.Ext(base))
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
  onix theme [name]             pick/apply visual theme (onix.visual.*.toml)
  onix themes list              list available visual themes
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
