//go:build windows

package opener

import (
	"fmt"
	"os/exec"
	"syscall"
)

// OpenFileWithDefault launches path using the Windows default program
// association, equivalent to double-clicking the file in Explorer.
func OpenFileWithDefault(path string) error {
	cmd := exec.Command("cmd.exe", "/C", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// OpenInExplorer opens path in Windows Explorer with the file selected.
func OpenInExplorer(path string) error {
	cmd := exec.Command("cmd.exe", "/C", "start", "explorer.exe", fmt.Sprintf(`/select,"%s"`, path))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
