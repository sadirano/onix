package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
	modKernel32                   = syscall.NewLazyDLL("kernel32.dll")
	modNtdll                      = syscall.NewLazyDLL("ntdll.dll")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
	procNtQueryInformationProcess  = modNtdll.NewProc("NtQueryInformationProcess")
)

const processQueryLimitedInformation = 0x1000

// parentPID returns the parent process ID of pid using NtQueryInformationProcess.
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

// procName returns the lowercase base name (e.g. "explorer.exe") of pid.
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

// launchedFromRunner reports whether onix was started from the Windows Run
// dialog (Win+R) rather than from an interactive terminal. It checks whether
// the direct parent or the grandparent process is explorer.exe.
//
// Uses targeted OpenProcess + NtQueryInformationProcess calls instead of a
// full process snapshot to keep the overhead under ~1 ms.
func launchedFromRunner() bool {
	ppid := uint32(os.Getppid())
	if procName(ppid) == "explorer.exe" {
		return true
	}
	// Typical Win+R chain: explorer.exe → cmd.exe → c.cmd → onix.exe
	gpid, ok := parentPID(ppid)
	if !ok {
		return false
	}
	return procName(gpid) == "explorer.exe"
}

// isUNCPath reports whether path is a UNC network path (\\server\share\...).
func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`)
}

// openShellAt opens an interactive cmd.exe at dir. For UNC paths it uses
// pushd to map the share to a temporary drive letter, since cmd.exe cannot
// cd to a UNC path directly.
//
// When launched from Win+R the parent chain leads back to explorer.exe: onix
// starts cmd.exe with inherited console handles and exits immediately so that
// cmd.exe owns the console window directly. When launched from an existing
// terminal onix blocks until the shell exits (normal interactive behaviour).
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
		// Break the child out of any job object so it can outlive onix.
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x01000000} // CREATE_BREAKAWAY_FROM_JOB
		return cmd.Start()
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}

func promptSubdir(segName string) string {
	fmt.Printf("Subdirectory for segment %q: ", segName)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type readResult struct{ line string }
	ch := make(chan readResult, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- readResult{line}
	}()

	select {
	case <-sig:
		fmt.Println()
		return ""
	case r := <-ch:
		return strings.TrimSpace(r.line)
	}
}

func promptDestination(aliasName string) string {
	fmt.Printf("Destination for %q: ", aliasName)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type readResult struct{ line string }
	ch := make(chan readResult, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- readResult{line}
	}()

	select {
	case <-sig:
		fmt.Println() // move cursor to a clean line before the shell prompt reappears
		return ""
	case r := <-ch:
		return strings.TrimSpace(r.line)
	}
}
