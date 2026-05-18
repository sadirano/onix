//go:build !windows

package opener

import "fmt"

// OpenFileWithDefault is not supported on non-Windows platforms.
func OpenFileWithDefault(path string) error {
	return fmt.Errorf("OpenFileWithDefault: not supported on this platform")
}

// OpenInExplorer is not supported on non-Windows platforms.
func OpenInExplorer(path string) error {
	return fmt.Errorf("OpenInExplorer: not supported on this platform")
}
