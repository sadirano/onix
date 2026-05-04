// Package errs defines shared error types used across onix internal packages.
package errs

import "fmt"

// ExitError is returned when a child process exits with a non-zero code.
// main.go checks for this type and calls os.Exit with the embedded code,
// allowing deferred cleanup to run before the process terminates.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}
