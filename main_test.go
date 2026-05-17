package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasFlag(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		names []string
		want  bool
	}{
		{"match", []string{"--json"}, []string{"--json", "-j"}, true},
		{"match short", []string{"-j"}, []string{"--json", "-j"}, true},
		{"no match", []string{"--list"}, []string{"--json", "-j"}, false},
		{"prefix match (should fail)", []string{"--json=foo"}, []string{"--json"}, false},
		{"empty", []string{}, []string{"--json"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasFlag(tt.args, tt.names...); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ONIX_HOME", tempHome)

	runOnix := func(args ...string) (int, string, string) {
		var outBuf, errBuf bytes.Buffer
		code := run(args, strings.NewReader(""), &outBuf, &errBuf)
		return code, outBuf.String(), errBuf.String()
	}

	t.Run("version", func(t *testing.T) {
		code, out, _ := runOnix("onix", "--version")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "onix") {
			t.Errorf("output should contain 'onix', got %q", out)
		}
	})

	t.Run("help", func(t *testing.T) {
		code, out, _ := runOnix("onix", "--help")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "USAGE:") {
			t.Errorf("output should contain 'USAGE:', got %q", out)
		}
	})

	t.Run("bare usage", func(t *testing.T) {
		code, out, _ := runOnix("onix")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "USAGE:") {
			t.Errorf("output should contain 'USAGE:', got %q", out)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		code, _, err := runOnix("onix", "--bogus")
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(err, "unknown flag") {
			t.Errorf("stderr should contain 'unknown flag', got %q", err)
		}
	})

	t.Run("plugin ls empty", func(t *testing.T) {
		// Create an empty config.toml so plugin ls doesn't fail on "no home"
		_ = os.MkdirAll(tempHome, 0o755)
		_ = os.WriteFile(filepath.Join(tempHome, "config.toml"), []byte("version = 2\n"), 0o644)

		code, out, errOut := runOnix("onix", "plugin", "ls")
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		output := out + errOut
		if !strings.Contains(output, "no plugins installed") {
			t.Errorf("output should say 'no plugins installed', got %q", output)
		}
	})

	t.Run("plugin ls override home", func(t *testing.T) {
		otherHome := t.TempDir()
		_ = os.WriteFile(filepath.Join(otherHome, "config.toml"), []byte("version = 2\n"), 0o644)

		code, out, errOut := runOnix("onix", "plugin", "ls", "--config-dir", otherHome)
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		output := out + errOut
		if !strings.Contains(output, "no plugins installed") {
			t.Errorf("output should say 'no plugins installed', got %q", output)
		}
	})
}
