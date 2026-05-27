package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// Checkpoint timer — activated by ONIX_TIMING=1.

type checkpoint struct {
	name    string
	elapsed time.Duration
	delta   time.Duration
	alloc   uint64 // heap bytes currently allocated
	total   uint64 // cumulative heap bytes allocated
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

func (t *timer) Mark(name string) {
	if t == nil {
		return
	}
	t.mark(name)
}

func (t *timer) mark(name string) {
	if t == nil || !t.enabled {
		return
	}
	now := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	t.checkpoints = append(t.checkpoints, checkpoint{
		name:    name,
		elapsed: now.Sub(t.start),
		delta:   now.Sub(t.last),
		alloc:   m.Alloc,
		total:   m.TotalAlloc,
	})
	t.last = now
}

func (t *timer) report() {
	if !t.enabled || len(t.checkpoints) == 0 {
		return
	}
	total := time.Since(t.start)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintln(os.Stderr, "\n[ONIX TIMING] -----------------------------------------------------------------------")
	fmt.Fprintf(os.Stderr, "  %-24s  %10s  %10s  %10s  %10s\n", "phase", "delta", "elapsed", "heap", "total_alloc")
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("-", 71))
	for _, cp := range t.checkpoints {
		fmt.Fprintf(
			os.Stderr, "  %-24s  %10s  %10s  %10s  %10s\n",
			cp.name,
			fmtDur(cp.delta),
			fmtDur(cp.elapsed),
			fmtBytes(cp.alloc),
			fmtBytes(cp.total),
		)
	}
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("-", 71))
	fmt.Fprintf(os.Stderr, "  %-24s  %10s  %10s  %10s  %10s\n", "TOTAL", fmtDur(total), "", "", fmtBytes(m.TotalAlloc))
	fmt.Fprintln(os.Stderr, "[ONIX TIMING] -----------------------------------------------------------------------")
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
}

func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
