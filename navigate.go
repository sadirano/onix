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

	"github.com/sadirano/onix/internal/config"
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

// processCommandLineInformation is the NtQueryInformationProcess class that
// returns the process command line as a UNICODE_STRING.
const processCommandLineInformation = 60

// procCmdLine reads the command line of pid using ProcessCommandLineInformation.
// Returns "" on any failure. The UNICODE_STRING buffer returned by the kernel
// is self-contained: Buffer points within the same allocation we pass in.
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
	// UNICODE_STRING layout (64-bit): Length uint16, MaxLength uint16, [4 pad], Buffer *uint16
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
	// Grandparent is explorer.exe — check whether the parent cmd.exe is
	// non-interactive (/C one-shot) rather than an interactive terminal.
	parentLine := strings.ToLower(procCmdLine(ppid))
	hasC := strings.Contains(parentLine, " /c ")
	hasK := strings.Contains(parentLine, " /k ")
	return hasC && !hasK
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

// readLine prints prompt and reads one line from stdin.
// Returns ("", false) when the user cancels with Ctrl+C or a stream error occurs.
func readLine(prompt string) (string, bool) {
	fmt.Print(prompt)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// Create a new reader on every call to avoid buffer state issues.
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- result{line, err}
	}()

	select {
	case <-sig:
		fmt.Println()
		os.Exit(1) // Terminate immediately to avoid "Terminate batch job" loops.
		return "", false
	case r := <-ch:
		if r.err != nil {
			return "", false
		}
		return strings.TrimSpace(r.line), true
	}
}

// promptContextConfig interactively asks the user to configure a context for
// segName. Returns the filled ContextConfig and true, or (zero, false) on
// Ctrl+C. The config is NOT written here — the caller is responsible for that.
func promptContextConfig(segName string) (config.ContextConfig, bool) {
	fmt.Printf("No context configured for segment %q\n", segName)

	var source string
	for {
		s, ok := readLine("  source [env/cmd/file/alias]: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		switch s {
		case "env", "cmd", "file", "alias":
			source = s
		default:
			fmt.Fprintf(os.Stderr, "  unknown source %q — choose env, cmd, file, or alias\n", s)
			continue
		}
		break
	}

	cc := config.ContextConfig{Source: source}
	switch source {
	case "alias":
		path, ok := readLine("  path: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Path = path

	case "env":
		v, ok := readLine("  var: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Var = v
		tmpl, ok := readLine("  template (optional): ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Template = tmpl

	case "cmd":
		cmd, ok := readLine("  command: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Cmd = cmd
		tmpl, ok := readLine("  template (optional): ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Template = tmpl

	case "file":
		f, ok := readLine("  file: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.File = f
		tmpl, ok := readLine("  template (optional): ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Template = tmpl
	}

	return cc, true
}

func promptDestination(aliasName string) string {
	line, ok := readLine(fmt.Sprintf("Destination for %q: ", aliasName))
	if !ok {
		return ""
	}
	return line
}
