package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixNow pins the package clock to a fixed instant and restores it on
// cleanup. Returns the pinned time.
func fixNow(t *testing.T, at time.Time) {
	t.Helper()
	prev := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = prev })
}

func TestRecordAndLoad(t *testing.T) {
	home := t.TempDir()
	at := time.Unix(1_700_000_000, 0)
	fixNow(t, at)

	Record(home, "Acme") // mixed case must normalise to the store's lowercase keys

	got := Load(home)
	e, ok := got["acme"]
	if !ok {
		t.Fatalf("entry missing after Record: %v", got)
	}
	if e.Count != 1 || e.Last != at.Unix() {
		t.Errorf("entry = %+v, want Count=1 Last=%d", e, at.Unix())
	}
}

func TestRecordDebounce(t *testing.T) {
	home := t.TempDir()
	t0 := time.Unix(1_700_000_000, 0)

	fixNow(t, t0)
	Record(home, "acme")

	// Inside the window: a pure read, no bump.
	fixNow(t, t0.Add(30*time.Minute))
	Record(home, "acme")
	if e := Load(home)["acme"]; e.Count != 1 || e.Last != t0.Unix() {
		t.Errorf("debounced record changed entry: %+v", e)
	}

	// Past the window: bump count and stamp.
	t2 := t0.Add(2 * time.Hour)
	fixNow(t, t2)
	Record(home, "acme")
	if e := Load(home)["acme"]; e.Count != 2 || e.Last != t2.Unix() {
		t.Errorf("post-window record = %+v, want Count=2 Last=%d", e, t2.Unix())
	}
}

func TestRecordEmptyNameIsNoop(t *testing.T) {
	home := t.TempDir()
	Record(home, "   ")
	if _, err := os.Stat(Path(home)); !os.IsNotExist(err) {
		t.Error("empty alias name created a usage file")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if got := Load(t.TempDir()); len(got) != 0 {
		t.Errorf("missing file should load empty, got %v", got)
	}
}

func TestLoadSkipsMalformedLines(t *testing.T) {
	home := t.TempDir()
	raw := "acme 3 1700000000\n" +
		"toofew 1\n" +
		"notanumber x 1700000000\n" +
		"negative -1 1700000000\n" +
		"trailing 1 1700000000 extra\n" +
		"\n" +
		"sms 7 1700000500\n"
	if err := os.WriteFile(filepath.Join(home, "usage"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Load(home)
	if len(got) != 2 {
		t.Fatalf("want 2 valid entries, got %v", got)
	}
	if got["acme"].Count != 3 || got["sms"].Count != 7 {
		t.Errorf("valid entries mangled: %v", got)
	}
}

func TestRemove(t *testing.T) {
	home := t.TempDir()
	fixNow(t, time.Unix(1_700_000_000, 0))
	Record(home, "acme")
	Record(home, "sms")

	Remove(home, []string{"ACME", "never-recorded"})

	got := Load(home)
	if _, ok := got["acme"]; ok {
		t.Error("acme still present after Remove")
	}
	if _, ok := got["sms"]; !ok {
		t.Error("sms lost by Remove of unrelated names")
	}
}
