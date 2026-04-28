package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Checkpoint timer — activated by ONIX_TIMING=1.

type checkpoint struct {
	name    string
	elapsed time.Duration
	delta   time.Duration
}

type timer struct {
	enabled     bool
	start       time.Time
	last        time.Time
	checkpoints []checkpoint
}

func newTimer() *timer {
	t := &timer{
		enabled: os.Getenv("ONIX_TIMING") == "1",
		start:   time.Now(),
	}
	t.last = t.start
	return t
}

func (t *timer) mark(name string) {
	if !t.enabled {
		return
	}
	now := time.Now()
	t.checkpoints = append(t.checkpoints, checkpoint{
		name:    name,
		elapsed: now.Sub(t.start),
		delta:   now.Sub(t.last),
	})
	t.last = now
}

func (t *timer) report() {
	if !t.enabled || len(t.checkpoints) == 0 {
		return
	}
	total := time.Since(t.start)
	fmt.Fprintln(os.Stderr, "\n[ONIX TIMING] ----------------------------------------")
	fmt.Fprintf(os.Stderr, "  %-28s  %12s  %12s\n", "phase", "delta", "elapsed")
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("-", 56))
	for _, cp := range t.checkpoints {
		fmt.Fprintf(os.Stderr, "  %-28s  %12s  %12s\n", cp.name, fmtDur(cp.delta), fmtDur(cp.elapsed))
	}
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("-", 56))
	fmt.Fprintf(os.Stderr, "  %-28s  %12s\n", "TOTAL", fmtDur(total))
	fmt.Fprintln(os.Stderr, "[ONIX TIMING] ----------------------------------------")
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.3fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
}
