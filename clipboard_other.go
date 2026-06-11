//go:build !windows

package main

import "fmt"

// readClipboardContent is Windows-only: image/text clipboard reading uses
// golang.design/x/clipboard, which requires cgo and X11 headers on Linux.
// The path write-back (atotto/clipboard) still works cross-platform, but the
// read side of --paste is not wired up off Windows.
func readClipboardContent() (data []byte, defaultExt string, err error) {
	return nil, "", fmt.Errorf("onix --paste: reading the clipboard is only supported on Windows")
}

// readClipboardFiles is Windows-only too: file drops (CF_HDROP) have no
// portable equivalent here. Returning nil makes --paste fall through to
// the content path and its platform error above.
func readClipboardFiles() []string {
	return nil
}
