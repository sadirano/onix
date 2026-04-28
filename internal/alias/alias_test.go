package alias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyEnvOverride(t *testing.T) {
	clearAliasEnv := func(t *testing.T) {
		t.Helper()
		for _, k := range []string{"ONIX_ENV", "ONIX_ALIAS_FILE", "OMNI_ENV", "OMNI_ALIAS_FILE"} {
			t.Setenv(k, "")
		}
	}

	t.Run("sets ONIX_ALIAS_FILE when no env var is set", func(t *testing.T) {
		clearAliasEnv(t)
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "/my/aliases" {
			t.Errorf("got %q, want /my/aliases", got)
		}
	})
	t.Run("no-op when aliasFile is empty", func(t *testing.T) {
		clearAliasEnv(t)
		ApplyEnvOverride("")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("expected no change, got %q", got)
		}
	})
	t.Run("no-op when aliasFile is whitespace-only", func(t *testing.T) {
		clearAliasEnv(t)
		ApplyEnvOverride("   ")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("expected no change, got %q", got)
		}
	})
	t.Run("no-op when ONIX_ENV already set", func(t *testing.T) {
		clearAliasEnv(t)
		t.Setenv("ONIX_ENV", "/already/set")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("ONIX_ALIAS_FILE should be untouched, got %q", got)
		}
	})
	t.Run("no-op when ONIX_ALIAS_FILE already set", func(t *testing.T) {
		clearAliasEnv(t)
		t.Setenv("ONIX_ALIAS_FILE", "/existing")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "/existing" {
			t.Errorf("expected existing value preserved, got %q", got)
		}
	})
	t.Run("no-op when OMNI_ENV already set", func(t *testing.T) {
		clearAliasEnv(t)
		t.Setenv("OMNI_ENV", "/omni/set")
		ApplyEnvOverride("/my/aliases")
		if got := os.Getenv("ONIX_ALIAS_FILE"); got != "" {
			t.Errorf("ONIX_ALIAS_FILE should be untouched, got %q", got)
		}
	})
}

func TestFilePath(t *testing.T) {
	t.Run("ONIX_ENV takes priority", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "/custom/path")
		t.Setenv("ONIX_ALIAS_FILE", "/other/path")
		if got := FilePath(); got != "/custom/path" {
			t.Errorf("got %q, want /custom/path", got)
		}
	})
	t.Run("ONIX_ALIAS_FILE second priority", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "")
		t.Setenv("ONIX_ALIAS_FILE", "/alias/file")
		t.Setenv("OMNI_ENV", "/omni/path")
		if got := FilePath(); got != "/alias/file" {
			t.Errorf("got %q, want /alias/file", got)
		}
	})
	t.Run("OMNI_ENV third priority", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "")
		t.Setenv("ONIX_ALIAS_FILE", "")
		t.Setenv("OMNI_ENV", "/omni/path")
		if got := FilePath(); got != "/omni/path" {
			t.Errorf("got %q, want /omni/path", got)
		}
	})
	t.Run("default path contains .omni/.env", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "")
		t.Setenv("ONIX_ALIAS_FILE", "")
		t.Setenv("OMNI_ENV", "")
		t.Setenv("OMNI_ALIAS_FILE", "")
		got := FilePath()
		if !strings.HasSuffix(got, filepath.Join(".omni", ".env")) {
			t.Errorf("got %q, expected suffix %q", got, filepath.Join(".omni", ".env"))
		}
	})
	t.Run("whitespace-only env var falls through", func(t *testing.T) {
		t.Setenv("ONIX_ENV", "   ")
		t.Setenv("ONIX_ALIAS_FILE", "/alias/file")
		if got := FilePath(); got != "/alias/file" {
			t.Errorf("got %q, want /alias/file", got)
		}
	})
}
