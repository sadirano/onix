//go:build !windows

package main

import "os"

// consoleIO returns file handles connected to the controlling terminal and a
// cleanup func. See console_windows.go for the rationale. On POSIX the
// controlling terminal is /dev/tty; if it can't be opened we fall back to the
// inherited std handles so non-interactive callers still work.
func consoleIO() (in, out *os.File, cleanup func()) {
	if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		return f, f, func() { f.Close() }
	}
	return os.Stdin, os.Stdout, func() {}
}
