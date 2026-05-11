//go:build !windows

package main

import "fmt"

func openExplorer(target string) error {
	return fmt.Errorf("openExplorer: not supported on this platform")
}
