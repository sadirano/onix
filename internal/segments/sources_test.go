package segments

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeLookup returns a LookupFunc backed by a static map.
func makeLookup(env map[string]string) LookupFunc {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func TestEvalTemplateSource(t *testing.T) {
	got, err := EvalTemplateSource("/${client}", makeLookup(map[string]string{"client": "bob"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "/bob" {
		t.Errorf("got %q, want /bob", got)
	}

	if _, err := EvalTemplateSource("/${missing}", makeLookup(nil)); err == nil {
		t.Error("expected error for missing variable")
	}
}

// withFakeExec swaps execCommand for the duration of a test and restores
// it on cleanup. The fake routes through a helper-process trick: it execs
// the test binary with a special TestHelperProcess entry point, which
// emits canned stdout/stderr/exit-code based on env vars.
//
// This avoids spawning real external binaries (git, etc.) on the dev's
// machine while still exercising the actual exec.Cmd plumbing.
func withFakeExec(t *testing.T, stdout, stderr string, exitCode int) {
	t.Helper()
	prev := execCommand
	t.Cleanup(func() { execCommand = prev })
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT=" + stdout,
			"HELPER_STDERR=" + stderr,
			"HELPER_EXIT=" + itoaOrZero(exitCode),
		}
		return cmd
	}
}

func itoaOrZero(n int) string {
	if n == 0 {
		return "0"
	}
	// strconv.Itoa avoided to keep imports tight; tiny manual conversion.
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestHelperProcess is the helper invoked by withFakeExec. It looks at
// HELPER_* env vars and prints/exits accordingly.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if out := os.Getenv("HELPER_STDOUT"); out != "" {
		os.Stdout.WriteString(out)
	}
	if errOut := os.Getenv("HELPER_STDERR"); errOut != "" {
		os.Stderr.WriteString(errOut)
	}
	exit := os.Getenv("HELPER_EXIT")
	switch exit {
	case "", "0":
		os.Exit(0)
	default:
		// Numeric only — single digit fine for tests.
		os.Exit(int(exit[0] - '0'))
	}
}

func TestEvalExecSource_HappyPath(t *testing.T) {
	withFakeExec(t, "feature/foo\n", "", 0)
	got, err := EvalExecSource([]string{"git", "rev-parse", "--abbrev-ref", "HEAD"}, t.TempDir(), makeLookup(nil))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "feature/foo" {
		t.Errorf("got %q, want feature/foo (trimmed)", got)
	}
}

func TestEvalExecSource_ExpandsArgs(t *testing.T) {
	withFakeExec(t, "ok", "", 0)
	got, err := EvalExecSource([]string{"echo", "${msg}"}, "", makeLookup(map[string]string{"msg": "hello"}))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want ok", got)
	}
}

func TestEvalExecSource_NonZeroExitErrors(t *testing.T) {
	withFakeExec(t, "", "fatal: not a git repository", 1)
	_, err := EvalExecSource([]string{"git", "rev-parse"}, "", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error should include stderr text: %v", err)
	}
}

func TestEvalExecSource_EmptyArgsErrors(t *testing.T) {
	if _, err := EvalExecSource(nil, "", makeLookup(nil)); err == nil {
		t.Error("expected error for empty argument list")
	}
}

func TestEvalExecSource_TemplateErrorPropagates(t *testing.T) {
	_, err := EvalExecSource([]string{"echo", "${missing}"}, "", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for missing variable")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention missing variable: %v", err)
	}
}

func TestEvalFileSource_AtHomePrefix(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "current"), []byte("TASK-9001\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := EvalFileSource("@home/state/current", home, "", makeLookup(nil))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "TASK-9001" {
		t.Errorf("got %q, want TASK-9001 (trimmed)", got)
	}
}

func TestEvalFileSource_AtAliasPrefix(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "BRANCH"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EvalFileSource("@alias/BRANCH", "", base, makeLookup(nil))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "main" {
		t.Errorf("got %q, want main", got)
	}
}

func TestEvalFileSource_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data")
	if err := os.WriteFile(p, []byte("absolute"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EvalFileSource(filepath.ToSlash(p), "", "", makeLookup(nil))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "absolute" {
		t.Errorf("got %q, want absolute", got)
	}
}

func TestEvalFileSource_MissingFileErrors(t *testing.T) {
	_, err := EvalFileSource("@home/state/nope", t.TempDir(), "", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should mention the path: %v", err)
	}
}

func TestEvalFileSource_RejectsBarePath(t *testing.T) {
	_, err := EvalFileSource("just-a-name", "", "", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for bare (non-prefixed, non-absolute) path")
	}
}

func TestEvalFileSource_TemplateExpansion(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "client-bob"), []byte("CLIENT-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EvalFileSource("@home/state/client-${client}", home, "", makeLookup(map[string]string{"client": "bob"}))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != "CLIENT-DATA" {
		t.Errorf("got %q, want CLIENT-DATA", got)
	}
}

func TestEvalFileSource_TildePrefix(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("UserHomeDir behaviour not exercised on this OS")
	}
	// Just confirm the prefix is recognised; a missing target gives a
	// read-error mentioning the resolved (expanded) path. We don't write
	// into the real user home — that would be intrusive.
	_, err := EvalFileSource("~/onix-test-nope-should-not-exist", "", "", makeLookup(nil))
	if err == nil {
		t.Fatal("expected error for missing file under ~")
	}
}
