package main

import (
	"strings"
	"testing"
	"time"
)

func TestTimer_DisabledByDefault(t *testing.T) {
	t.Setenv("ONIX_TIMING", "")
	tm := newTimer()
	if tm.enabled {
		t.Error("timer should be disabled when ONIX_TIMING is unset")
	}

	// Disabled mark/report are no-ops; we capture stderr to confirm silence.
	_, stderr, err := captureStdio(func() error {
		tm.mark("phase1")
		tm.report()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("disabled timer wrote to stderr: %q", stderr)
	}
	if len(tm.checkpoints) != 0 {
		t.Errorf("disabled timer recorded %d checkpoints, want 0", len(tm.checkpoints))
	}
}

func TestTimer_EnabledRecordsAndReports(t *testing.T) {
	t.Setenv("ONIX_TIMING", "1")
	tm := newTimer()
	if !tm.enabled {
		t.Fatal("timer should be enabled when ONIX_TIMING=1")
	}

	tm.mark("phase1")
	tm.mark("phase2")
	if len(tm.checkpoints) != 2 {
		t.Fatalf("got %d checkpoints, want 2", len(tm.checkpoints))
	}

	_, stderr, err := captureStdio(func() error {
		tm.report()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "ONIX TIMING") {
		t.Errorf("report missing header: %q", stderr)
	}
	if !strings.Contains(stderr, "phase1") || !strings.Contains(stderr, "phase2") {
		t.Errorf("report missing phase names: %q", stderr)
	}
	if !strings.Contains(stderr, "TOTAL") {
		t.Errorf("report missing TOTAL line: %q", stderr)
	}
}

func TestTimer_ReportSkippedWithNoCheckpoints(t *testing.T) {
	t.Setenv("ONIX_TIMING", "1")
	tm := newTimer()
	_, stderr, err := captureStdio(func() error {
		tm.report()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("report with zero checkpoints wrote: %q", stderr)
	}
}

func TestFmtDur(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Nanosecond, "ns"},
		{50 * time.Microsecond, "µs"},
		{50 * time.Millisecond, "ms"},
		{2 * time.Second, "s"},
	}
	for _, tc := range tests {
		got := fmtDur(tc.d)
		if !strings.Contains(got, tc.want) {
			t.Errorf("fmtDur(%v) = %q, want suffix %q", tc.d, got, tc.want)
		}
	}
}
