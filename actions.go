package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/sadirano/onix/internal/config"
	"github.com/sadirano/onix/internal/errs"
	"github.com/sadirano/onix/internal/opener"
)

// executeAction carries out the resolved action against target.
// Returns *errs.ExitError when a child process exits with a non-zero code so
// that main can forward the exit code without calling os.Exit from a library.
func executeAction(act *config.Action, target string, extras []string, cfg *config.Config, t *timer) error {
	if act.Lua != "" {
		return executeLuaAction(act.Lua, target, extras)
	}

	switch act.Builtin {
	case "explorer":
		return openExplorer(target)

	case "editor":
		return opener.RunEditorCommand(cfg.ResolveEditor(), target, ".")

	case "print":
		fmt.Println(target)
		clipCmd := cfg.ResolveClipboardCmd()
		c := exec.Command(clipCmd)
		c.Stdin = strings.NewReader(target)
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not copy to clipboard: %v\n", err)
		}
		lastVar := cfg.ResolveLastVar()
		if runtime.GOOS == "windows" {
			if err := exec.Command("setx", lastVar, target).Run(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not set %s: %v\n", lastVar, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "note: %s not persisted (setx is Windows-only)\n", lastVar)
		}
		return nil

	case "files":
		if len(extras) == 0 || (len(extras) == 1 && extras[0] == ".") {
			return opener.RunEditorCommand(cfg.ResolveEditor(), target, ".")
		}
		resolved := make([]string, len(extras))
		for i, arg := range extras {
			if filepath.IsAbs(arg) {
				resolved[i] = arg
			} else {
				resolved[i] = filepath.Join(target, arg)
			}
		}
		return opener.OpenMixedFiles(cfg.ResolveEditor(), resolved)

	case "run":
		if cfg.Settings.DisableRun {
			return fmt.Errorf("run builtin is disabled (disable_run = true in config)")
		}
		if len(extras) == 0 {
			return fmt.Errorf("usage: run <alias> \"<command>\"")
		}
		var rcmd *exec.Cmd
		if runtime.GOOS == "windows" {
			if isUNCPath(target) {
				wrapped := fmt.Sprintf(`pushd "%s" && %s`, target, strings.Join(extras, " "))
				rcmd = exec.Command("cmd.exe", "/C", wrapped)
			} else {
				rcmd = exec.Command("cmd.exe", "/C", strings.Join(extras, " "))
				rcmd.Dir = target
			}
		} else {
			rcmd = exec.Command("sh", "-c", strings.Join(extras, " "))
			rcmd.Dir = target
		}
		rcmd.Stdout = os.Stdout
		rcmd.Stderr = os.Stderr
		rcmd.Stdin = os.Stdin
		if err := rcmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ProcessState != nil {
				return &errs.ExitError{Code: exitErr.ProcessState.ExitCode()}
			}
			return fmt.Errorf("run command: %w", err)
		}
		return nil

	default: // "shell" and anything unrecognised
		t.mark("shell spawned")
		return openShellAt(target, cfg.ResolveShell())
	}
}

// executeLuaAction runs an inline Lua action function with (target, args).
// src must be a Lua chunk that returns a function.
func executeLuaAction(src, target string, args []string) error {
	L := lua.NewState()
	defer L.Close()
	if err := L.DoString(src); err != nil {
		return fmt.Errorf("lua action: %w", err)
	}
	fn, ok := L.Get(-1).(*lua.LFunction)
	if !ok {
		return fmt.Errorf("lua action: must return a function, got %T", L.Get(-1))
	}
	L.SetTop(0)
	argsTable := L.NewTable()
	for i, a := range args {
		L.RawSetInt(argsTable, i+1, lua.LString(a))
	}
	return L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	}, lua.LString(target), argsTable)
}
