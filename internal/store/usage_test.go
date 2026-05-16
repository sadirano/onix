package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsage(t *testing.T) {
	home := t.TempDir()

	// 1. Record some usage.
	if err := RecordUsage(home, "acme"); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := RecordUsage(home, "beta"); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := RecordUsage(home, "acme"); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	// 2. Check scores.
	scores := GetFrecencyScores(home)
	if scores["acme"] != 20 { // 2 x 10pts (since they are fresh)
		t.Errorf("acme score = %v, want 20", scores["acme"])
	}
	if scores["beta"] != 10 {
		t.Errorf("beta score = %v, want 10", scores["beta"])
	}

	// 3. Test recency multipliers.
	// Manual append to log with old timestamps.
	p := filepath.Join(home, "usage.log")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	
	now := time.Now().Unix()
	// 2 hours ago (weight 5)
	fmt.Fprintf(f, "%d,%s\n", now-7200, "oldie")
	// 2 days ago (weight 2)
	fmt.Fprintf(f, "%d,%s\n", now-(86400*2), "ancient")
	// 2 weeks ago (weight 1)
	fmt.Fprintf(f, "%d,%s\n", now-(86400*14), "fossil")
	f.Close()

	scores = GetFrecencyScores(home)
	if scores["oldie"] != 5 {
		t.Errorf("oldie score = %v, want 5", scores["oldie"])
	}
	if scores["ancient"] != 2 {
		t.Errorf("ancient score = %v, want 2", scores["ancient"])
	}
	if scores["fossil"] != 1 {
		t.Errorf("fossil score = %v, want 1", scores["fossil"])
	}
}
