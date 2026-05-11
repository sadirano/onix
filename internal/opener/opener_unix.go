//go:build !windows

package opener

import "fmt"

func OpenFileWithDefault(path string) error {
	return fmt.Errorf("OpenFileWithDefault: not supported on this platform")
}

func OpenInExplorer(path string) error {
	return fmt.Errorf("OpenInExplorer: not supported on this platform")
}
