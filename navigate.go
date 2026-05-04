package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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

func promptDestination(aliasName string) string {
	fmt.Printf("Destination for %q: ", aliasName)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type readResult struct{ line string }
	ch := make(chan readResult, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- readResult{line}
	}()

	select {
	case <-sig:
		fmt.Println() // move cursor to a clean line before the shell prompt reappears
		return ""
	case r := <-ch:
		return strings.TrimSpace(r.line)
	}
}
