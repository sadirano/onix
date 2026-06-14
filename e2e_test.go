package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var onixExe string

func TestMain(m *testing.M) {
	flag.Parse()

	tmp, err := os.MkdirTemp("", "onix-e2e-build-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	exe := filepath.Join(tmp, "onix.exe")
	if runtime.GOOS != "windows" {
		exe = filepath.Join(tmp, "onix")
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", exe, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build onix: %v\n%s\n", err, out)
		os.Exit(1)
	}
	onixExe = exe

	os.Exit(m.Run())
}

type onixRunner struct {
	t    *testing.T
	home string
	exe  string
}

func (r *onixRunner) run(args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command(r.exe, args...)
	cmd.Env = append(os.Environ(), "ONIX_HOME="+r.home)
	var outB, errB bytes.Buffer
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	err = cmd.Run()
	return outB.String(), errB.String(), err
}

func TestE2E_BasicFlow(t *testing.T) {
	home := t.TempDir()
	r := &onixRunner{t: t, home: home, exe: onixExe}

	// 1. Init
	t.Run("init", func(t *testing.T) {
		stdout, stderr, err := r.run("--init", "--skip-profile")
		if err != nil {
			t.Fatalf("init failed: %v", err)
		}
		output := stdout + stderr
		if !strings.Contains(output, "onix home") {
			t.Errorf("unexpected init output: %q", output)
		}
	})

	// 2. Add
	demoDir := t.TempDir()
	t.Run("add", func(t *testing.T) {
		out, _, err := r.run("demo", demoDir)
		if err != nil {
			t.Fatalf("add failed: %v", err)
		}
		if !strings.Contains(filepath.ToSlash(out), filepath.ToSlash(demoDir)) {
			t.Errorf("expected path %q in output, got %q", demoDir, out)
		}
	})

	// 3. Resolve
	t.Run("resolve", func(t *testing.T) {
		out, _, err := r.run("demo")
		if err != nil {
			t.Fatalf("resolve failed: %v", err)
		}
		got := filepath.ToSlash(strings.TrimSpace(out))
		want := filepath.ToSlash(demoDir)
		if got != want {
			t.Errorf("resolve demo = %q, want %q", got, want)
		}
	})

	// 4. List
	t.Run("list", func(t *testing.T) {
		out, _, err := r.run("--list")
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if !strings.Contains(out, "demo") {
			t.Errorf("list output missing 'demo': %q", out)
		}
	})
}

func TestE2E_ShellIntegration_PowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell integration test only runs on Windows")
	}
	pwsh, err := exec.LookPath("pwsh.exe")
	if err != nil {
		pwsh, err = exec.LookPath("powershell.exe")
	}
	if err != nil {
		t.Skip("PowerShell not found")
	}

	home := t.TempDir()
	r := &onixRunner{t: t, home: home, exe: onixExe}

	// 1. Init to generate snippet
	if out, _, err := r.run("--init", "--skip-profile"); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// 2. Add an alias
	demoDir := t.TempDir()
	if out, _, err := r.run("demo", demoDir); err != nil {
		t.Fatalf("add failed: %v\n%s", err, out)
	}

	// 3. The snippet no longer defines an `o` function — `o` is now the
	// installed o.exe wrapper, resolved via the wrapper dir the snippet
	// prepends to PATH. Navigation opens a fresh shell rooted at the target,
	// so we point ONIX_SHELL at a stub that prints its working dir and exits;
	// that lets us observe where `o demo` navigated without an interactive
	// session. This exercises the full chain: PATH prepend -> o.exe ->
	// argv[0] dispatch -> resolve -> subshell rooted at the alias dir.
	snip := filepath.Join(home, "shell", "onix.ps1")
	if _, err := os.Stat(snip); err != nil {
		t.Fatalf("snippet missing at %s: %v", snip, err)
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "o.exe")); err != nil {
		t.Fatalf("o.exe wrapper not installed: %v", err)
	}

	stub := filepath.Join(home, "navstub.cmd")
	if err := os.WriteFile(stub, []byte("@echo off\r\necho NAV:%CD%\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	script := fmt.Sprintf(`. '%s'; $env:ONIX_SHELL = '%s'; o demo`, snip, stub)
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "ONIX_HOME="+home)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh failed: %v\nOutput:\n%s", err, out)
	}

	var nav string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "NAV:") {
			nav = strings.TrimSpace(strings.TrimPrefix(line, "NAV:"))
		}
	}
	if nav == "" {
		t.Fatalf("no NAV: line from the navigation subshell:\n%s", out)
	}
	// On Windows CI, t.TempDir() can return an 8.3 short-name path while the
	// subshell's %CD% returns the long form. Canonicalise both sides.
	if !strings.EqualFold(canonPath(t, nav), canonPath(t, demoDir)) {
		t.Errorf("pwsh 'o demo' navigated to %q (canon %q), want %q (canon %q)\nFull Output:\n%s",
			nav, canonPath(t, nav), demoDir, canonPath(t, demoDir), out)
	}
}

func canonPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(resolved)
}

func TestE2E_ShellIntegration_Bash(t *testing.T) {
	if runtime.GOOS == "windows" {
		// --init is platform-aware and writes onix.ps1 (not onix.sh) on
		// Windows, so sourcing the bash snippet from Git Bash here would
		// always fail. Bash snippet behaviour is covered by the
		// ubuntu-latest test job.
		t.Skip("bash snippet not generated on Windows; covered by Linux job")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}

	home := t.TempDir()
	r := &onixRunner{t: t, home: home, exe: onixExe}

	if out, err := exec.Command(bash, "-c", "echo ok").CombinedOutput(); err != nil {
		t.Skipf("bash is not usable (maybe WSL not configured): %v\n%s", err, out)
	}

	_, _, _ = r.run("--init", "--skip-profile")

	demoDir := t.TempDir()
	_, _, _ = r.run("demo", demoDir)

	snip := filepath.Join(home, "shell", "onix.sh")
	script := fmt.Sprintf(`source '%s'; o demo && pwd`, filepath.ToSlash(snip))

	cmd := exec.Command(bash, "-c", script)
	cmd.Env = append(os.Environ(), "ONIX_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash failed: %v\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	if filepath.ToSlash(got) != filepath.ToSlash(demoDir) {
		t.Errorf("bash 'o demo' changed to %q, want %q", got, demoDir)
	}
}
