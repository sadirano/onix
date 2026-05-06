package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// InstallShortcuts writes .cmd wrappers in ~/.onix/bin/ for each configured
// action. Each wrapper sets ONIX_COMMAND to the action name before calling onix.
func InstallShortcuts(cfg *config.Config) error {
	return InstallShortcutsProfile("", cfg)
}

// InstallShortcutsProfile writes .cmd wrappers using a named built-in profile
// when profile is non-empty, otherwise falls back to the config actions (or
// DefaultActions when none are declared).
// When a profile is used its actions are also written into config.toml so that
// resolveBuiltin can look them up at runtime.
func InstallShortcutsProfile(profile string, cfg *config.Config) error {
	var actions []config.Action
	if profile != "" {
		p, ok := config.BuiltinProfiles[profile]
		if !ok {
			return fmt.Errorf("unknown shortcut profile %q — available: %v", profile, profileNames())
		}
		actions = p
		fmt.Printf("Using shortcut profile %q\n", profile)
		cfg.Actions = actions
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save profile actions to config: %w", err)
		}
	} else {
		actions = cfg.Actions
		if len(actions) == 0 {
			actions = config.DefaultActions
		}
	}
	return installShortcutsFromActions(actions)
}

func installShortcutsFromActions(actions []config.Action) error {
	sorted := make([]config.Action, len(actions))
	copy(sorted, actions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	onixExe, err := resolveOnixExe()
	if err != nil {
		return err
	}

	var warnings []string
	for _, a := range sorted {
		if err := createCommandWrapper(a.Name, onixExe, config.BinDir()); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s.cmd skipped: %v", a.Name, err))
			fmt.Printf("  ! %s.cmd (skipped: %v)\n", a.Name, err)
			continue
		}
		fmt.Printf("  %s.cmd\n", a.Name)
	}
	binDir := config.BinDir()
	fmt.Printf("Shortcuts installed in %s\n", binDir)
	if len(warnings) > 0 {
		fmt.Println("Warnings:")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
		fmt.Println("Close related shells and run `onix install` again to refresh.")
	}

	if err := addBinToUserPath(binDir); err != nil {
		fmt.Printf("Warning: could not update PATH automatically: %v\n", err)
		fmt.Printf("Add manually: %s\n", binDir)
	}
	return nil
}

func profileNames() []string {
	names := make([]string, 0, len(config.BuiltinProfiles))
	for k := range config.BuiltinProfiles {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// addBinToUserPath adds dir to the user-scoped PATH in the Windows registry.
// The directory is passed as an environment variable to avoid any PowerShell
// quoting or injection issues with special characters in the path.
func addBinToUserPath(dir string) error {
	script := `
$dir = $env:ONIX_ADD_PATH
$current = [Environment]::GetEnvironmentVariable("Path", "User")
$parts = $current -split ";" | Where-Object { $_.Trim() -ne "" }
if ($parts -contains $dir) {
    Write-Host "PATH already contains $dir"
} else {
    $new = ($parts + $dir) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $new, "User")
    Write-Host "Added to PATH: $dir"
    Write-Host "Restart your terminal for the change to take effect."
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "ONIX_ADD_PATH="+dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createCommandWrapper(name, onixExe, _ string) error {
	return writeCmdWrapper(name, onixExe, map[string]string{"ONIX_COMMAND": name})
}

func createWrapper(name string) error {
	onixExe, err := resolveOnixExe()
	if err != nil {
		return err
	}
	return writeCmdWrapper(name, onixExe, map[string]string{"ONIX_MODULE": name})
}

// createEntryWrapper writes a .cmd wrapper that sets both ONIX_MODULE and ONIX_ENTRY.
func createEntryWrapper(moduleName, entryName, cmdName string) error {
	onixExe, err := resolveOnixExe()
	if err != nil {
		return err
	}
	return writeCmdWrapper(cmdName, onixExe, map[string]string{
		"ONIX_MODULE": moduleName,
		"ONIX_ENTRY":  entryName,
	})
}

// resolveOnixExe returns the absolute path of the currently running onix binary.
func resolveOnixExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve onix executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", fmt.Errorf("resolve onix executable path: %w", err)
	}
	return exe, nil
}

// writeCmdWrapper writes a .cmd wrapper to BinDir that sets the given env vars
// before calling onix. The exe path is stored in a SET variable so that any
// percent signs in the path are not misinterpreted by cmd.exe.
func writeCmdWrapper(name, onixExe string, envVars map[string]string) error {
	if err := os.MkdirAll(config.BinDir(), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("@echo off\r\nsetlocal\r\n")
	sb.WriteString("set \"ONIX_EXE=" + onixExe + "\"\r\n")
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString("set \"" + k + "=" + envVars[k] + "\"\r\n")
	}
	sb.WriteString("\"%ONIX_EXE%\" %*\r\nset \"ONIX_EXIT=%ERRORLEVEL%\"\r\nendlocal & exit /b %ONIX_EXIT%\r\n")
	return os.WriteFile(filepath.Join(config.BinDir(), name+".cmd"), []byte(sb.String()), 0o644)
}

// extractCmdVar parses a line of the form  set "VARNAME=value"  from content
// and returns value. Returns "" when the variable is not found.
func extractCmdVar(content, varName string) string {
	prefix := `set "` + varName + `=`
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.TrimSuffix(val, `"`)
			return val
		}
	}
	return ""
}

// checkCmdConflict returns an error when the .cmd file at BinDir/cmdName.cmd
// already exists and is owned by a different module/entry.
func checkCmdConflict(cmdName, moduleName, entryName string) error {
	p := filepath.Join(config.BinDir(), cmdName+".cmd")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing wrapper %s: %w", cmdName+".cmd", err)
	}
	content := string(data)
	existingModule := extractCmdVar(content, "ONIX_MODULE")
	existingEntry := extractCmdVar(content, "ONIX_ENTRY")
	if existingModule == moduleName && existingEntry == entryName {
		return nil
	}
	if existingModule != "" {
		return fmt.Errorf("cmd %q is already owned by module %q (entry %q) — remove that module first", cmdName, existingModule, existingEntry)
	}
	return nil
}

// removeModuleWrappers removes all .cmd files in BinDir whose ONIX_MODULE
// variable matches moduleName.
func removeModuleWrappers(moduleName string) error {
	binDir := config.BinDir()
	entries, err := os.ReadDir(binDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".cmd") {
			continue
		}
		p := filepath.Join(binDir, de.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if extractCmdVar(string(data), "ONIX_MODULE") == moduleName {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", de.Name(), err)
			}
		}
	}
	return nil
}
