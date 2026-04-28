package visual

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the visual configuration for onix core.
// Only the destination picker is managed here; sg/ff/rg settings are owned
// by their respective module binaries and read from the same file independently.
type Config struct {
	FZF FZFConfig `toml:"fzf"`
}

// FZFConfig holds per-picker fzf settings for core-managed pickers.
type FZFConfig struct {
	Destination PickerConfig `toml:"destination"`
}

// PickerConfig holds fzf flags for a single picker.
type PickerConfig struct {
	Prompt        string `toml:"prompt"`
	Layout        string `toml:"layout"`
	Color         string `toml:"color"`
	Preview       string `toml:"preview"`
	PreviewWindow string `toml:"preview_window"`
	Header        string `toml:"header"`
	Height        string `toml:"height"`
}

// ConfigFileName is the filename for the active visual config file.
const ConfigFileName = "onix.visual.toml"

// ConfigStarter is the default content written when no config file exists.
const ConfigStarter = `# onix.visual.toml

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

// Default returns the built-in visual configuration defaults.
func Default() Config {
	return Config{
		FZF: FZFConfig{
			Destination: PickerConfig{
				Prompt:        "Destination > ",
				Layout:        "reverse-list",
				Preview:       `dir /b "{}" 2>nul`,
				PreviewWindow: "right:40%,border-left",
				Header:        "Enter to confirm  |  Esc to type manually",
				Height:        "60%",
			},
		},
	}
}

// Load reads (and creates if missing) onix.visual.toml from binDir.
// binDir is the directory containing onix.exe, resolved by the caller.
func Load(binDir string) (Config, string, error) {
	cfg := Default()
	if strings.TrimSpace(binDir) == "" {
		return cfg, "", nil
	}

	configPath := filepath.Join(binDir, ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if writeErr := os.WriteFile(configPath, []byte(ConfigStarter), 0o644); writeErr != nil {
			return cfg, configPath, fmt.Errorf("create %s: %w", configPath, writeErr)
		}
		return cfg, configPath, nil
	} else if err != nil {
		return cfg, configPath, err
	}

	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return Default(), configPath, fmt.Errorf("decode %s: %w", configPath, err)
	}
	cfg.ApplyDefaults()
	return cfg, configPath, nil
}

// ApplyDefaults fills in zero-value fields with the built-in defaults.
func (v *Config) ApplyDefaults() {
	def := Default()
	v.FZF.Destination.Prompt = fallback(v.FZF.Destination.Prompt, def.FZF.Destination.Prompt)
	v.FZF.Destination.Layout = fallback(v.FZF.Destination.Layout, def.FZF.Destination.Layout)
	v.FZF.Destination.Preview = fallback(v.FZF.Destination.Preview, def.FZF.Destination.Preview)
	v.FZF.Destination.PreviewWindow = fallback(v.FZF.Destination.PreviewWindow, def.FZF.Destination.PreviewWindow)
	v.FZF.Destination.Header = fallback(v.FZF.Destination.Header, def.FZF.Destination.Header)
	v.FZF.Destination.Height = fallback(v.FZF.Destination.Height, def.FZF.Destination.Height)
}

// AppendLayoutArg appends --layout <layout> to args unless layout is empty or "default".
func AppendLayoutArg(args []string, layout string) []string {
	layout = strings.TrimSpace(layout)
	if layout == "" || strings.EqualFold(layout, "default") {
		return args
	}
	return append(args, "--layout", layout)
}

// ---------------------------------------------------------------------------
// Theme management
// ---------------------------------------------------------------------------

// HandleThemeCommand implements `onix theme [name|list]`.
// binDir is the directory containing onix.exe (and the theme files).
func HandleThemeCommand(args []string, binDir string, debug bool) error {
	activePath := filepath.Join(binDir, ConfigFileName)
	if err := EnsureConfigFile(activePath); err != nil {
		return err
	}

	themes, err := ListThemes(binDir)
	if err != nil {
		return err
	}
	if len(themes) == 0 {
		return fmt.Errorf("no theme files found in %s", binDir)
	}

	if len(args) > 0 {
		if strings.EqualFold(args[0], "list") || strings.EqualFold(args[0], "ls") {
			current := DetectActiveTheme(activePath, themes)
			for _, t := range themes {
				tag := ""
				if t == current {
					tag = " (current)"
				}
				fmt.Printf("%s%s\n", filepath.Base(t), tag)
			}
			return nil
		}
		selected, err := ResolveThemeArg(strings.TrimSpace(args[0]), themes)
		if err != nil {
			return err
		}
		return ApplyTheme(selected, activePath, debug)
	}

	selected, err := PickThemeInteractive(themes, activePath)
	if err != nil {
		return err
	}
	if selected == "" {
		return nil
	}
	return ApplyTheme(selected, activePath, debug)
}

// EnsureConfigFile writes the starter config to path if it does not exist.
func EnsureConfigFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(ConfigStarter), 0o644); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

// ListThemes returns all onix.visual.*.toml files in dir, sorted.
func ListThemes(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "onix.visual.*.toml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// ResolveThemeArg finds the theme path matching input (by full name or stem).
func ResolveThemeArg(input string, themes []string) (string, error) {
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

// PickThemeInteractive presents a theme picker, using fzf if available.
func PickThemeInteractive(themes []string, activePath string) (string, error) {
	current := DetectActiveTheme(activePath, themes)
	if _, err := exec.LookPath("fzf"); err == nil {
		return PickThemeWithFZF(themes, current)
	}
	return PickThemeWithPrompt(themes, current)
}

// DetectActiveTheme returns the theme path whose content matches activePath, or "".
func DetectActiveTheme(activePath string, themes []string) string {
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

// PickThemeWithFZF presents an fzf picker for themes.
func PickThemeWithFZF(themes []string, current string) (string, error) {
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

// PickThemeWithPrompt presents a numbered text prompt for theme selection.
func PickThemeWithPrompt(themes []string, current string) (string, error) {
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

// ApplyTheme copies themePath over activePath.
func ApplyTheme(themePath, activePath string, debug bool) error {
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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}
