package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/plugins"
)

// fakeExecResponse describes one scripted reply for the swap-in execCommand.
// fakeExec consumes responses in FIFO order, and records each invocation's
// argv into fakeExecCalls so tests can assert against them.
type fakeExecResponse struct {
	stdout string
	stderr string
	exit   int
}

var (
	fakeExecQueue []fakeExecResponse
	fakeExecCalls [][]string
)

// installFakeExec swaps execCommand AND execCommandContext for the duration
// of t and resets the shared queue / recorded-calls state. Tests push
// expected responses with pushFakeResponse and inspect calls via
// fakeExecCalls.
func installFakeExec(t *testing.T) {
	t.Helper()
	prevCmd := execCommand
	prevCtx := execCommandContext
	fakeExecQueue = nil
	fakeExecCalls = nil

	makeFakeCmd := func(name string, args ...string) *exec.Cmd {
		fakeExecCalls = append(fakeExecCalls, append([]string{name}, args...))
		var resp fakeExecResponse
		if len(fakeExecQueue) > 0 {
			resp = fakeExecQueue[0]
			fakeExecQueue = fakeExecQueue[1:]
		}
		cs := []string{"-test.run=TestPluginInstallHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		// Inherit env so the subprocess's TestMain has TMP/PATH/GOROOT
		// available; the GO_WANT_HELPER_PROCESS sentinel makes TestMain
		// short-circuit before any real work.
		cmd.Env = append(
			os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT="+resp.stdout,
			"HELPER_STDERR="+resp.stderr,
			"HELPER_EXIT="+strconv.Itoa(resp.exit),
		)
		return cmd
	}
	execCommand = makeFakeCmd
	execCommandContext = func(_ context.Context, name string, args ...string) *exec.Cmd {
		return makeFakeCmd(name, args...)
	}
	t.Cleanup(func() {
		execCommand = prevCmd
		execCommandContext = prevCtx
		fakeExecQueue = nil
		fakeExecCalls = nil
	})
}

func pushFakeResponse(stdout, stderr string, exit int) {
	fakeExecQueue = append(fakeExecQueue, fakeExecResponse{stdout: stdout, stderr: stderr, exit: exit})
}

// TestPluginInstallHelperProcess is exec'd as a subprocess by installFakeExec.
// It emits the HELPER_* envvars to stdout/stderr and exits with HELPER_EXIT,
// letting the parent test observe whatever the production code does with
// those streams and exit codes.
func TestPluginInstallHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if out := os.Getenv("HELPER_STDOUT"); out != "" {
		_, _ = io.WriteString(os.Stdout, out)
	}
	if errOut := os.Getenv("HELPER_STDERR"); errOut != "" {
		_, _ = io.WriteString(os.Stderr, errOut)
	}
	exit, _ := strconv.Atoi(os.Getenv("HELPER_EXIT"))
	os.Exit(exit)
}

// findCall returns the first recorded invocation whose argv contains all
// fragments (in order). Returns nil if none matched.
func findCall(fragments ...string) []string {
	for _, call := range fakeExecCalls {
		joined := strings.Join(call, " ")
		ok := true
		idx := 0
		for _, frag := range fragments {
			found := strings.Index(joined[idx:], frag)
			if found < 0 {
				ok = false
				break
			}
			idx += found + len(frag)
		}
		if ok {
			return call
		}
	}
	return nil
}

func TestGitFetch(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("", "", 0)
	if err := gitFetch("/tmp/repo"); err != nil {
		t.Fatalf("gitFetch: %v", err)
	}
	call := findCall("git", "-C", "/tmp/repo", "fetch", "--depth=50", "origin")
	if call == nil {
		t.Errorf("expected `git -C /tmp/repo fetch --depth=50 origin`, got %v", fakeExecCalls)
	}
}

func TestGitCheckout_DefaultBranch(t *testing.T) {
	installFakeExec(t)
	// First call: symbolic-ref returns the branch name (with trailing \n).
	pushFakeResponse("main\n", "", 0)
	// Second call: reset --hard origin/main succeeds.
	pushFakeResponse("", "", 0)

	if err := gitCheckout("/tmp/repo", ""); err != nil {
		t.Fatalf("gitCheckout(\"\"): %v", err)
	}
	if findCall("symbolic-ref", "--short", "HEAD") == nil {
		t.Errorf("symbolic-ref not invoked: %v", fakeExecCalls)
	}
	if findCall("reset", "--hard", "origin/main") == nil {
		t.Errorf("reset --hard origin/main not invoked: %v", fakeExecCalls)
	}
}

func TestGitCheckout_DefaultBranch_SymbolicRefFails(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("", "fatal: not a symbolic ref", 1)
	err := gitCheckout("/tmp/repo", "")
	if err == nil {
		t.Fatal("expected error when symbolic-ref fails")
	}
	if !strings.Contains(err.Error(), "resolve default branch") {
		t.Errorf("error should mention default-branch resolution: %v", err)
	}
}

func TestGitCheckout_ExplicitRef(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("", "", 0)
	if err := gitCheckout("/tmp/repo", "abc123"); err != nil {
		t.Fatalf("gitCheckout(abc123): %v", err)
	}
	if findCall("reset", "--hard", "abc123") == nil {
		t.Errorf("reset --hard abc123 not invoked: %v", fakeExecCalls)
	}
}

func TestGitCheckout_ExplicitRef_Fails(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("", "fatal: bad revision", 128)
	err := gitCheckout("/tmp/repo", "deadbeef")
	if err == nil {
		t.Fatal("expected error when reset fails")
	}
	if !strings.Contains(err.Error(), "deadbeef") {
		t.Errorf("error should mention the ref: %v", err)
	}
}

func TestGitHeadSHA(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("  abc123def456\n", "", 0)
	got, err := gitHeadSHA("/tmp/repo")
	if err != nil {
		t.Fatalf("gitHeadSHA: %v", err)
	}
	if got != "abc123def456" {
		t.Errorf("got %q, want abc123def456 (whitespace-trimmed)", got)
	}
}

func TestGitHeadSHA_Fails(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("", "fatal: not a git repo", 128)
	_, err := gitHeadSHA("/tmp/repo")
	if err == nil {
		t.Fatal("expected error from gitHeadSHA")
	}
}

func TestGitHeadMessage(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("hello commit\n", "", 0)
	got, err := gitHeadMessage("/tmp/repo")
	if err != nil {
		t.Fatalf("gitHeadMessage: %v", err)
	}
	if got != "hello commit" {
		t.Errorf("got %q, want hello commit", got)
	}
}

func TestGitHeadMessage_Fails(t *testing.T) {
	installFakeExec(t)
	pushFakeResponse("", "fatal: no head", 1)
	_, err := gitHeadMessage("/tmp/repo")
	if err == nil {
		t.Fatal("expected error from gitHeadMessage")
	}
}

func TestBuildPlugin_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	err := buildPlugin(dir, "out.exe")
	if err == nil {
		t.Fatal("expected error for dir missing go.mod")
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("error should mention go.mod: %v", err)
	}
}

func TestBuildPlugin_Success(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installFakeExec(t)
	pushFakeResponse("", "", 0)

	if err := buildPlugin(dir, "out.exe"); err != nil {
		t.Fatalf("buildPlugin: %v", err)
	}
	call := findCall("go", "build", "-o", "out.exe")
	if call == nil {
		t.Errorf("expected `go build ... -o out.exe`, got %v", fakeExecCalls)
	}
}

func TestBuildPlugin_BuildFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installFakeExec(t)
	pushFakeResponse("", "syntax error", 1)

	err := buildPlugin(dir, "out.exe")
	if err == nil {
		t.Fatal("expected error when go build fails")
	}
	if !strings.Contains(err.Error(), "go build") {
		t.Errorf("error should mention go build: %v", err)
	}
}

// confirmInstall accepts single-letter `y`/`Y` only (intentionally strict —
// `yes` and other strings reject so the user's full word doesn't sneak past
// when they meant to cancel).
func TestConfirmInstall_YesAccepts(t *testing.T) {
	for _, ans := range []string{"y\n", "Y\n", "  y  \n"} {
		t.Run(strings.TrimSpace(ans), func(t *testing.T) {
			var out bytes.Buffer
			got := confirmInstall(strings.NewReader(ans), &out, "user/repo", "wrap", "abc", "msg", nil, false)
			if !got {
				t.Errorf("input %q should accept; output was:\n%s", ans, out.String())
			}
			if !strings.Contains(out.String(), "user/repo") {
				t.Errorf("output should echo the repo: %q", out.String())
			}
		})
	}
}

func TestConfirmInstall_NoRejects(t *testing.T) {
	for _, ans := range []string{"n\n", "no\n", "\n", "bogus\n", "yes\n"} {
		t.Run(strings.TrimSpace(ans), func(t *testing.T) {
			var out bytes.Buffer
			if confirmInstall(strings.NewReader(ans), &out, "user/repo", "wrap", "abc", "", nil, false) {
				t.Errorf("input %q should reject", ans)
			}
		})
	}
}

func TestConfirmInstall_EOFRejects(t *testing.T) {
	var out bytes.Buffer
	if confirmInstall(strings.NewReader(""), &out, "user/repo", "wrap", "abc", "", nil, false) {
		t.Error("EOF should reject")
	}
}

func TestConfirmInstall_UnpinnedBannerShown(t *testing.T) {
	var out bytes.Buffer
	confirmInstall(strings.NewReader("n\n"), &out, "user/repo", "wrap",
		"abcdef0123456789", "", nil, true)
	body := out.String()
	if !strings.Contains(body, "UNPINNED") {
		t.Errorf("unpinned banner missing: %q", body)
	}
	if !strings.Contains(body, "abcdef012345") {
		t.Errorf("12-char SHA prefix missing: %q", body)
	}
	if !strings.Contains(body, "tracks default branch") {
		t.Errorf("unpinned warning copy missing: %q", body)
	}
}

func TestConfirmInstall_EntriesListed(t *testing.T) {
	var out bytes.Buffer
	entries := []plugins.PluginEntry{
		{Name: "start"},
		{Name: "stop", Cmd: "x-stop"},
	}
	confirmInstall(strings.NewReader("y\n"), &out, "u/r", "wrap", "abc", "feat", entries, false)
	body := out.String()
	if !strings.Contains(body, "entries:") {
		t.Errorf("entries label missing: %q", body)
	}
	if !strings.Contains(body, "start") || !strings.Contains(body, "x-stop") {
		t.Errorf("entry names missing: %q", body)
	}
	if !strings.Contains(body, "commit:") || !strings.Contains(body, "feat") {
		t.Errorf("commit line missing: %q", body)
	}
}
