package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeUsageLog seeds a usage.log file with the given (offset, alias) pairs,
// where offset is the duration before now. Offsets are stored as absolute
// unix seconds so the file format matches what RecordUsage writes.
func writeUsageLog(t *testing.T, home string, entries []struct {
	ago   time.Duration
	alias string
}) {
	t.Helper()
	path := filepath.Join(home, "usage.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	now := time.Now()
	for _, e := range entries {
		ts := now.Add(-e.ago).Unix()
		fmt.Fprintf(f, "%d,%s\n", ts, e.alias)
	}
}

func TestStatsCmd_Empty(t *testing.T) {
	home := t.TempDir()
	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Top: 10}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatalf("StatsCmd.Run: %v", err)
	}
	if !strings.Contains(stdout, "no activity") {
		t.Errorf("expected empty-state message, got: %q", stdout)
	}
}

func TestStatsCmd_Default(t *testing.T) {
	home := t.TempDir()
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "acme"},
		{2 * time.Hour, "acme"},
		{30 * time.Minute, "acme"},
		{1 * time.Hour, "omni"},
		{8 * 24 * time.Hour, "old"}, // outside this-week
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Top: 10}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatalf("StatsCmd.Run: %v", err)
	}

	for _, want := range []string{"Activity:", "All time:", "Top", "acme", "omni"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatsCmd_TopOrdering(t *testing.T) {
	home := t.TempDir()
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "winner"},
		{2 * time.Hour, "winner"},
		{3 * time.Hour, "winner"},
		{1 * time.Hour, "second"},
		{2 * time.Hour, "second"},
		{1 * time.Hour, "third"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Top: 10}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}

	// "winner" should appear before "second", which should appear before "third".
	wi := strings.Index(stdout, "winner")
	si := strings.Index(stdout, "second")
	ti := strings.Index(stdout, "third")
	if wi < 0 || si < 0 || ti < 0 {
		t.Fatalf("missing alias in output:\n%s", stdout)
	}
	if !(wi < si && si < ti) {
		t.Errorf("ranking is wrong (positions: winner=%d, second=%d, third=%d):\n%s",
			wi, si, ti, stdout)
	}
}

func TestStatsCmd_JSON(t *testing.T) {
	home := t.TempDir()
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "acme"},
		{1 * time.Hour, "omni"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Top: 10}).Run(context.Background(), &env{Home: home, JSON: true})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "{") {
		t.Errorf("expected JSON object, got: %q", stdout)
	}
	for _, want := range []string{`"total"`, `"top"`, `"acme"`, `"omni"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatsCmd_SinceFilter(t *testing.T) {
	home := t.TempDir()
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "fresh"},
		{48 * time.Hour, "stale"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Top: 10, Since: "24h"}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "fresh") {
		t.Errorf("fresh alias missing from windowed view:\n%s", stdout)
	}
	if strings.Contains(stdout, "stale") {
		t.Errorf("stale alias should be excluded by --since 24h:\n%s", stdout)
	}
}

func TestStatsCmd_ColdView(t *testing.T) {
	home := t.TempDir()
	// Register three aliases.
	for _, name := range []string{"used", "neglected", "verydead"} {
		_ = (&AddCmd{Alias: name, Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	}
	// Only one of them shows recent usage.
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "used"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Cold: true}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "neglected") || !strings.Contains(stdout, "verydead") {
		t.Errorf("cold view should list unused aliases:\n%s", stdout)
	}
	if strings.Contains(stdout, "  used\n") {
		t.Errorf("cold view should NOT list the recently-used alias:\n%s", stdout)
	}
}

func TestStatsCmd_ColdView_AllUsed(t *testing.T) {
	home := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "acme"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Cold: true}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "no cold aliases") {
		t.Errorf("expected 'no cold aliases' message, got: %q", stdout)
	}
}

func TestStatsCmd_ColdView_SegmentWarmsBase(t *testing.T) {
	home := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	// Only segmented use: `docs@acme`. Should still warm `acme`.
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "docs@acme"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Cold: true}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "no cold aliases") {
		t.Errorf("a segmented use should warm its base alias, got: %q", stdout)
	}
}

func TestStatsCmd_Full(t *testing.T) {
	home := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	_ = (&AddCmd{Alias: "cold", Path: t.TempDir()}).Run(context.Background(), &env{Home: home})
	writeUsageLog(t, home, []struct {
		ago   time.Duration
		alias string
	}{
		{1 * time.Hour, "acme"},
		{4 * time.Hour, "acme"},
	})

	stdout, _, err := captureStdio(func() error {
		return (&StatsCmd{Top: 10, Full: true}).Run(context.Background(), &env{Home: home})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Activity:", "Top", "Cold aliases", "cold", "Activity by hour"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--full output missing %q:\n%s", want, stdout)
		}
	}
}

func TestParseSince(t *testing.T) {
	tests := []struct {
		in       string
		want     time.Duration
		wantErr  bool
	}{
		{"", 0, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"2w", 2 * 7 * 24 * time.Hour, false},
		{"nonsense", 0, true},
		{"0d", 0, true},
	}
	for _, tc := range tests {
		got, err := parseSince(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseSince(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSince(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestHumanizeAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{30 * time.Hour, "yesterday"},
		{3 * 24 * time.Hour, "3d ago"},
		{2 * 7 * 24 * time.Hour, "2w ago"},
		{60 * 24 * time.Hour, "2mo ago"},
	}
	for _, tc := range tests {
		got := humanizeAgo(tc.d)
		if got != tc.want {
			t.Errorf("humanizeAgo(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
