//go:build windows

package main

import (
	"errors"
	"fmt"
	"sync"

	gclip "golang.design/x/clipboard"
)

// clipboardInitOnce guards golang.design/x/clipboard's one-time Init, which
// opens the OS clipboard handle. We only pay for it when --paste actually
// runs, never on the resolve hot path.
var clipboardInitOnce struct {
	sync.Once
	err error
}

// readClipboardContent returns the current clipboard payload plus the default
// extension for it: an image as PNG bytes (.png), otherwise text (.md). Image
// wins when both are present — it's the harder content to re-grab, and the
// text remains recoverable from clipboard history.
func readClipboardContent() (data []byte, defaultExt string, err error) {
	clipboardInitOnce.Do(func() { clipboardInitOnce.err = gclip.Init() })
	if clipboardInitOnce.err != nil {
		return nil, "", fmt.Errorf("init clipboard: %w", clipboardInitOnce.err)
	}
	if img := gclip.Read(gclip.FmtImage); len(img) > 0 {
		return img, ".png", nil
	}
	if txt := gclip.Read(gclip.FmtText); len(txt) > 0 {
		return txt, ".md", nil
	}
	return nil, "", errors.New("clipboard holds no image or text to paste")
}
