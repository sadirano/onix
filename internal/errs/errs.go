// Package errs defines shared error types used across onix internal packages.
package errs

import (
	"fmt"
	"os"
)

// Exit codes used by onix.
const (
	ExitOK       = 0
	ExitErr      = 1 // general error
	ExitUsage    = 2 // bad arguments / usage error
	ExitNotFound = 3 // alias or module not found
)

// ExitError is returned when a child process exits with a non-zero code.
// main.go checks for this type and calls os.Exit with the embedded code,
// allowing deferred cleanup to run before the process terminates.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// Fatal prints an error to stderr and exits with ExitErr.
func Fatal(format string, a ...any) {
	FatalCode(ExitErr, format, a...)
}

// FatalCode prints an error to stderr and exits with the given code.
func FatalCode(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "onix: "+format+"\n", a...)
	os.Exit(code)
}
