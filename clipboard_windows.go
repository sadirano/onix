//go:build windows

package main

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

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
	return nil, "", errors.New("clipboard holds no files, image, or text to paste")
}

// CF_HDROP is the clipboard format Explorer uses for copied files: the
// payload is a DROPFILES list of absolute paths.
const cfHDROP = 15

var (
	user32                         = syscall.NewLazyDLL("user32.dll")
	shell32                        = syscall.NewLazyDLL("shell32.dll")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procDragQueryFileW             = shell32.NewProc("DragQueryFileW")
)

// readClipboardFiles returns the absolute paths of files/directories copied
// to the clipboard (Explorer's Ctrl+C), or nil when the clipboard holds no
// file drop. Best-effort: any failure reads as "no files" so --paste falls
// through to the image/text path.
func readClipboardFiles() []string {
	if avail, _, _ := procIsClipboardFormatAvailable.Call(cfHDROP); avail == 0 {
		return nil
	}
	// The clipboard is a global mutex; another process holding it makes
	// OpenClipboard fail transiently, so retry briefly before giving up.
	opened := false
	for range 5 {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			opened = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !opened {
		return nil
	}
	defer procCloseClipboard.Call() //nolint:errcheck // best-effort cleanup

	hDrop, _, _ := procGetClipboardData.Call(cfHDROP)
	if hDrop == 0 {
		return nil
	}
	const allFiles = 0xFFFFFFFF
	count, _, _ := procDragQueryFileW.Call(hDrop, allFiles, 0, 0)
	files := make([]string, 0, count)
	for i := uintptr(0); i < count; i++ {
		n, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0) // length sans NUL
		if n == 0 {
			continue
		}
		buf := make([]uint16, n+1)
		procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), n+1) //nolint:errcheck
		if p := syscall.UTF16ToString(buf); p != "" {
			files = append(files, p)
		}
	}
	return files
}
