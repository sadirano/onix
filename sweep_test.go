package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

// sweepLines renders a synthetic es /ad listing: parent dirs plus n
// children each.
func sweepLines(parents map[string]int) string {
	var b strings.Builder
	for parent, n := range parents {
		b.WriteString(parent + "\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, `%s\sub%03d`+"\n", parent, i)
		}
	}
	return b.String()
}

func TestSweepAnalyze_FloodedParent(t *testing.T) {
	in := sweepLines(map[string]int{
		`C:\stuff\photos`: 150,
		`C:\stuff\repos`:  10,
	})
	cands, err := sweepAnalyze(strings.NewReader(in), nil, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].path != `C:\stuff\photos` || cands[0].count != 150 {
		t.Errorf("want only C:\\stuff\\photos(150), got %v", cands)
	}
}

func TestSweepAnalyze_ExcludedTreesDontCount(t *testing.T) {
	in := sweepLines(map[string]int{`C:\stuff\photos`: 150})
	cands, err := sweepAnalyze(strings.NewReader(in), []string{`\photos\`}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Errorf("already-excluded children still counted: %v", cands)
	}
}

func TestSweepAnalyze_AliasTargetProtected(t *testing.T) {
	in := sweepLines(map[string]int{`C:\stuff\projects`: 150})
	cands, err := sweepAnalyze(strings.NewReader(in),
		nil, []string{`C:\stuff\projects\sub007`}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Errorf("dir containing an alias target was offered: %v", cands)
	}
}

func TestSweepAnalyze_SiblingCollapse(t *testing.T) {
	in := sweepLines(map[string]int{
		`C:\stuff\photos\album-a`: 120,
		`C:\stuff\photos\album-b`: 130,
		`C:\stuff\photos\album-c`: 140,
	})
	cands, err := sweepAnalyze(strings.NewReader(in), nil, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].path != `C:\stuff\photos` {
		t.Fatalf("want collapsed C:\\stuff\\photos, got %v", cands)
	}
	if cands[0].count != 390 {
		t.Errorf("collapsed count = %d, want 390 (sum of children)", cands[0].count)
	}
}

func TestSweepAnalyze_DriveRootsNeverOffered(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&b, `C:\top%03d`+"\n", i)
		fmt.Fprintf(&b, `C:\Users\me%03d`+"\n", i)
	}
	cands, err := sweepAnalyze(strings.NewReader(b.String()), nil, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Errorf("drive root or depth-1 dir offered: %v", cands)
	}
}

func TestSweepFragment(t *testing.T) {
	if got := sweepFragment(`C:\stuff\photos`); got != `C:\stuff\photos\` {
		t.Errorf("plain path: got %q", got)
	}
	// Spaced paths can't keep the trailing backslash (es eats \" pairs).
	if got := sweepFragment(`C:\stuff\New folder`); got != `C:\stuff\New folder` {
		t.Errorf("spaced path: got %q", got)
	}
}

// stubSweepExec routes the sweep's two exec calls: es prints the fixture
// listing, fzf replays the given selection (or exits with a code).
func stubSweepExec(t *testing.T, esOutput string, fzf func() *exec.Cmd) {
	t.Helper()
	esFile := filepath.Join(t.TempDir(), "es.txt")
	if err := os.WriteFile(esFile, []byte(esOutput), 0o644); err != nil {
		t.Fatal(err)
	}
	prevLook, prevCtx := lookPath, execCommandContext
	t.Cleanup(func() { lookPath, execCommandContext = prevLook, prevCtx })
	lookPath = func(name string) (string, error) { return "C:/fake/" + name, nil }
	execCommandContext = func(_ context.Context, name string, _ ...string) *exec.Cmd {
		if name == "es" {
			return typeCmd(esFile)
		}
		return fzf()
	}
}

// typeCmd returns a command that prints the file's content — survives
// tabs and spaces that echo-based stubs mangle.
func typeCmd(path string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "type", path)
	}
	return exec.Command("cat", path)
}

func sweepTestHome(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	e := &env{Home: home, Stdout: io.Discard, Stderr: io.Discard, Stdin: os.Stdin, NoPrompt: true}
	if err := (&InitCmd{SkipProfile: true}).Run(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSweepCmd_AppliesSelection(t *testing.T) {
	e := sweepTestHome(t)
	e.NoPrompt = false
	sel := filepath.Join(t.TempDir(), "sel.txt")
	if err := os.WriteFile(sel, []byte("150\tC:\\stuff\\photos\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubSweepExec(t, sweepLines(map[string]int{`C:\stuff\photos`: 150}),
		func() *exec.Cmd { return typeCmd(sel) })

	if err := (&SweepCmd{}).Run(context.Background(), e); err != nil {
		t.Fatalf("SweepCmd.Run: %v", err)
	}
	swept, err := config.LoadSwept(e.Home)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 1 || swept[0] != `C:\stuff\photos\` {
		t.Errorf("swept file = %v, want [C:\\stuff\\photos\\]", swept)
	}
	reg, err := os.ReadFile(filepath.Join(e.Home, "bin", "register.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reg), `!path:C:\stuff\photos\`) {
		t.Errorf("register.cmd not regenerated with swept term:\n%s", reg)
	}
}

func TestSweepCmd_CancelWritesNothing(t *testing.T) {
	e := sweepTestHome(t)
	e.NoPrompt = false
	stubSweepExec(t, sweepLines(map[string]int{`C:\stuff\photos`: 150}),
		func() *exec.Cmd { return exitCmd(130) })

	if err := (&SweepCmd{}).Run(context.Background(), e); err != nil {
		t.Fatalf("cancelled sweep must not error: %v", err)
	}
	if _, err := os.Stat(config.SweptPath(e.Home)); !os.IsNotExist(err) {
		t.Error("cancelled sweep wrote the swept file")
	}
}

func TestSweepCmd_NoPromptPrintsOnly(t *testing.T) {
	e := sweepTestHome(t)
	var out strings.Builder
	e.Stdout = &out
	stubSweepExec(t, sweepLines(map[string]int{`C:\stuff\photos`: 150}),
		func() *exec.Cmd { t.Fatal("fzf must not run with --no-prompt"); return nil })

	if err := (&SweepCmd{}).Run(context.Background(), e); err != nil {
		t.Fatalf("SweepCmd.Run: %v", err)
	}
	if !strings.Contains(out.String(), `C:\stuff\photos`) {
		t.Errorf("ranking not printed: %q", out.String())
	}
	if _, err := os.Stat(config.SweptPath(e.Home)); !os.IsNotExist(err) {
		t.Error("--no-prompt sweep wrote the swept file")
	}
}
