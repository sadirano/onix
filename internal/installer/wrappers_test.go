package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/config"
)

// ---------------------------------------------------------------------------
// extractCmdVar
// ---------------------------------------------------------------------------

func TestExtractCmdVar(t *testing.T) {
	content := "@echo off\r\nsetlocal\r\nset \"ONIX_EXE=C:\\tools\\onix.exe\"\r\nset \"ONIX_MODULE=mymod\"\r\n\"%ONIX_EXE%\" %*\r\n"

	t.Run("extracts ONIX_EXE", func(t *testing.T) {
		if got := extractCmdVar(content, "ONIX_EXE"); got != `C:\tools\onix.exe` {
			t.Errorf("got %q", got)
		}
	})
	t.Run("extracts ONIX_MODULE", func(t *testing.T) {
		if got := extractCmdVar(content, "ONIX_MODULE"); got != "mymod" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("returns empty for absent var", func(t *testing.T) {
		if got := extractCmdVar(content, "ONIX_ENTRY"); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
	t.Run("handles LF-only content", func(t *testing.T) {
		lf := "set \"ONIX_MODULE=mod\"\n"
		if got := extractCmdVar(lf, "ONIX_MODULE"); got != "mod" {
			t.Errorf("got %q, want mod", got)
		}
	})
}

// ---------------------------------------------------------------------------
// writeCmdWrapper / ONIX_EXE pattern
// ---------------------------------------------------------------------------

func TestWriteCmdWrapper_Structure(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())

	err := writeCmdWrapper("testwrap", `C:\tools\onix.exe`, map[string]string{
		"ONIX_MODULE": "mymod",
	})
	if err != nil {
		t.Fatalf("writeCmdWrapper: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(config.BinDir(), "testwrap.cmd"))
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	content := string(data)

	t.Run("uses CRLF line endings", func(t *testing.T) {
		if !strings.Contains(content, "\r\n") {
			t.Error("expected CRLF line endings")
		}
	})
	t.Run("stores exe in ONIX_EXE variable", func(t *testing.T) {
		if !strings.Contains(content, `set "ONIX_EXE=`) {
			t.Error("expected SET ONIX_EXE= line")
		}
	})
	t.Run("invokes via %ONIX_EXE%", func(t *testing.T) {
		if !strings.Contains(content, `"%ONIX_EXE%"`) {
			t.Error("expected invocation via %ONIX_EXE%")
		}
	})
	t.Run("sets ONIX_MODULE", func(t *testing.T) {
		if extractCmdVar(content, "ONIX_MODULE") != "mymod" {
			t.Error("ONIX_MODULE not set correctly")
		}
	})
}

func TestWriteCmdWrapper_PercentInPath(t *testing.T) {
	// A path containing % must not be misinterpreted by cmd.exe.
	t.Setenv("USERPROFILE", t.TempDir())

	weirdPath := `C:\100%sure\onix.exe`
	if err := writeCmdWrapper("pcttest", weirdPath, map[string]string{"ONIX_MODULE": "x"}); err != nil {
		t.Fatalf("writeCmdWrapper: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(config.BinDir(), "pcttest.cmd"))
	content := string(data)
	if extractCmdVar(content, "ONIX_EXE") != weirdPath {
		t.Errorf("percent path corrupted: %q", extractCmdVar(content, "ONIX_EXE"))
	}
}

// ---------------------------------------------------------------------------
// checkCmdConflict
// ---------------------------------------------------------------------------

func TestCheckCmdConflict(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())

	t.Run("absent file returns nil", func(t *testing.T) {
		if err := checkCmdConflict("nonexistent", "mymod", ""); err != nil {
			t.Errorf("expected nil for absent file, got %v", err)
		}
	})

	t.Run("same owner returns nil", func(t *testing.T) {
		if err := writeCmdWrapper("owned", `C:\onix.exe`, map[string]string{
			"ONIX_MODULE": "mymod",
			"ONIX_ENTRY":  "",
		}); err != nil {
			t.Fatal(err)
		}
		if err := checkCmdConflict("owned", "mymod", ""); err != nil {
			t.Errorf("expected nil for same owner, got %v", err)
		}
	})

	t.Run("different module returns error", func(t *testing.T) {
		if err := writeCmdWrapper("conflict", `C:\onix.exe`, map[string]string{
			"ONIX_MODULE": "othermod",
		}); err != nil {
			t.Fatal(err)
		}
		err := checkCmdConflict("conflict", "mymod", "")
		if err == nil {
			t.Fatal("expected conflict error, got nil")
		}
		if !strings.Contains(err.Error(), "othermod") {
			t.Errorf("error should mention existing owner: %v", err)
		}
	})
}
