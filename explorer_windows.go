//go:build windows

package main

import (
	"fmt"
	"syscall"
)

// openInExplorer launches Windows Explorer at target.
// HideWindow keeps an empty console flash from appearing when onix is
// invoked from a GUI context (e.g. Win+R). We do NOT shell out via
// cmd.exe /C start — that's a 15ms wakeup tax we don't need.
func openInExplorer(target string) error {
	cmd := execCommand("explorer.exe", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("explorer.exe %s: %w", target, err)
	}
	// We deliberately don't Wait — explorer.exe returns exit code 1 for
	// successful opens and we'd report a phantom failure. Fire and forget.
	return nil
}

// runCommandOutside starts the command detached from the parent console and
// running in a new console window on Windows.
func runCommandOutside(dir string, exe string, args []string) error {
	cmd := execCommand(exe, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x10, // CREATE_NEW_CONSOLE
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	return nil
}

