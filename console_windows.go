package main

import "os"

// consoleIO returns file handles connected to the controlling terminal and a
// cleanup func to release them.
//
// The `o.cmd` navigation flow runs `onix <alias> > .last 2>nul`, redirecting
// our stdout into a state file and our stderr into NUL. An interactive child
// launched from that flow — the segment-definition editor — would otherwise
// inherit those redirected handles, and a terminal editor (nvim, vim) seeing a
// non-tty stdout breaks its UI. Routing the child to the real console fixes it.
// Falls back to os.Stdin/os.Stdout when the console can't be opened (a headless
// or fully redirected context), so non-interactive callers still work.
func consoleIO() (in, out *os.File, cleanup func()) {
	in, out = os.Stdin, os.Stdout
	var toClose []*os.File
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		in = f
		toClose = append(toClose, f)
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		out = f
		toClose = append(toClose, f)
	}
	return in, out, func() {
		for _, f := range toClose {
			f.Close()
		}
	}
}
