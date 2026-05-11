//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// openExplorer opens target in Windows Explorer. UNC paths use the /e flag
// which handles network shares more reliably than the bare path form.
func openExplorer(target string) error {
	var cmd *exec.Cmd
	if isUNCPath(target) {
		cmd = exec.Command("cmd.exe", "/C", "start", "", "explorer.exe", "/e,"+target)
	} else {
		cmd = exec.Command("cmd.exe", "/C", "start", "", "explorer.exe", target)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open explorer: %w", err)
	}
	return nil
}
