package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isUNCPath reports whether path is a UNC network path (\\server\share\...).
func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`)
}

// openShellAt opens an interactive cmd.exe at dir. For UNC paths it uses
// pushd to map the share to a temporary drive letter, since cmd.exe cannot
// cd to a UNC path directly.
func openShellAt(dir string) error {
	var cmd *exec.Cmd
	if isUNCPath(dir) {
		cmd = exec.Command("cmd.exe", "/K", fmt.Sprintf(`pushd "%s"`, dir))
	} else {
		cmd = exec.Command("cmd.exe", "/K")
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}

// selectDestination prompts the user to type a destination path for aliasName.
func selectDestination(aliasName string) string {
	return promptDestination(aliasName)
}

func promptDestination(aliasName string) string {
	fmt.Printf("Destination for %q: ", aliasName)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	// ReadString error (e.g. closed stdin) produces an empty string,
	// which the caller treats as "no destination provided".
	return strings.TrimSpace(line)
}
