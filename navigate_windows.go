//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// processBasicInformation mirrors PROCESS_BASIC_INFORMATION from ntdll.
// InheritedFromUniqueProcessID at offset 5 is the parent PID.
type processBasicInformation struct {
	_               uintptr
	PebBaseAddress  uintptr
	_               [2]uintptr
	UniqueProcessID uintptr
	ParentProcessID uintptr
}

var (
	modKernel32                    = syscall.NewLazyDLL("kernel32.dll")
	modNtdll                       = syscall.NewLazyDLL("ntdll.dll")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
	procNtQueryInformationProcess  = modNtdll.NewProc("NtQueryInformationProcess")
)

const processQueryLimitedInformation = 0x1000

func parentPID(pid uint32) (uint32, bool) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return 0, false
	}
	defer syscall.CloseHandle(h)
	var pbi processBasicInformation
	var retLen uint32
	r, _, _ := procNtQueryInformationProcess.Call(
		uintptr(h), 0,
		uintptr(unsafe.Pointer(&pbi)),
		unsafe.Sizeof(pbi),
		uintptr(unsafe.Pointer(&retLen)),
	)
	if r != 0 {
		return 0, false
	}
	return uint32(pbi.ParentProcessID), true
}

func procName(pid uint32) string {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	var buf [syscall.MAX_PATH]uint16
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImageNameW.Call(
		uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:size])))
}

const processCommandLineInformation = 60

func procCmdLine(pid uint32) string {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)

	var retLen uint32
	procNtQueryInformationProcess.Call(
		uintptr(h), processCommandLineInformation,
		0, 0,
		uintptr(unsafe.Pointer(&retLen)),
	)
	if retLen == 0 {
		retLen = 1024
	}
	buf := make([]byte, retLen)
	r, _, _ := procNtQueryInformationProcess.Call(
		uintptr(h), processCommandLineInformation,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&retLen)),
	)
	if r != 0 || len(buf) < 16 {
		return ""
	}
	length := *(*uint16)(unsafe.Pointer(&buf[0]))
	bufPtr := *(*uintptr)(unsafe.Pointer(&buf[8]))
	if bufPtr == 0 || length == 0 {
		return ""
	}
	base := uintptr(unsafe.Pointer(&buf[0]))
	if bufPtr < base || bufPtr+uintptr(length) > base+uintptr(len(buf)) {
		return ""
	}
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(bufPtr)), length/2)
	return syscall.UTF16ToString(chars)
}

// launchedFromRunner reports whether onix was started from the Windows Run
// dialog (Win+R) rather than from an interactive terminal.
//
// Win+R chain:  explorer.exe → cmd.exe /C <wrapper.cmd> → onix.exe
// Normal chain: explorer.exe → cmd.exe (interactive terminal) → onix.exe
//
// Both chains have explorer.exe as grandparent, so the grandparent name alone
// is not enough. We disambiguate by reading the parent cmd.exe command line:
// a Win+R launch uses /C (one-shot), an interactive terminal uses /K or none.
func launchedFromRunner() bool {
	ppid := uint32(os.Getppid())
	if procName(ppid) == "explorer.exe" {
		return true
	}
	gpid, ok := parentPID(ppid)
	if !ok {
		return false
	}
	if procName(gpid) != "explorer.exe" {
		return false
	}
	parentLine := strings.ToLower(procCmdLine(ppid))
	hasC := strings.Contains(parentLine, " /c ")
	hasK := strings.Contains(parentLine, " /k ")
	return hasC && !hasK
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

	if launchedFromRunner() {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x01000000} // CREATE_BREAKAWAY_FROM_JOB
		return cmd.Start()
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}
