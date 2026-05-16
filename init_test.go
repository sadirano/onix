package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCmd(t *testing.T) {
	home := t.TempDir()
	if err := (&InitCmd{SkipProfile: true}).Run(context.Background(), &env{Home: home}); err != nil {
		t.Fatalf("InitCmd.Run: %v", err)
	}

	// Verify aliases.toml exists
	if _, err := os.Stat(filepath.Join(home, "aliases.toml")); err != nil {
		t.Errorf("aliases.toml not created: %v", err)
	}

	// Verify snippet exists
	snippetFound := false
	_ = filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && (strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".ps1")) {
			snippetFound = true
		}
		return nil
	})
	if !snippetFound {
		t.Errorf("no shell snippet created in %s", home)
	}
}

func TestSourceFromBashLike(t *testing.T) {
	t.Run("appends to existing rc", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)

		rc := filepath.Join(home, ".bashrc")
		if err := os.WriteFile(rc, []byte("# pre-existing\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		snippetPath := filepath.Join(home, "shell", "onix.sh")
		_, stdout, err := captureStdio(func() error {
			return sourceFromBashLike(snippetPath)
		})
		_ = stdout
		if err != nil {
			t.Fatalf("sourceFromBashLike: %v", err)
		}

		updated, _ := os.ReadFile(rc)
		if !strings.Contains(string(updated), snippetPath) {
			t.Errorf("rc was not updated to source snippet:\n%s", updated)
		}
		if !strings.Contains(string(updated), "Added by 'onix init'") {
			t.Errorf("rc missing onix marker:\n%s", updated)
		}
	})

	t.Run("skips when already sourced", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)

		snippetPath := filepath.Join(home, "shell", "onix.sh")
		rc := filepath.Join(home, ".bashrc")
		body := "# pre\n. '" + snippetPath + "'\n"
		if err := os.WriteFile(rc, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}

		_, _, err := captureStdio(func() error {
			return sourceFromBashLike(snippetPath)
		})
		if err != nil {
			t.Fatalf("sourceFromBashLike: %v", err)
		}

		updated, _ := os.ReadFile(rc)
		// Should not have appended anything new.
		if strings.Count(string(updated), snippetPath) != 1 {
			t.Errorf("snippet appears %d times, want 1:\n%s",
				strings.Count(string(updated), snippetPath), updated)
		}
	})

	t.Run("warns when no rc files present", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)

		snippetPath := filepath.Join(home, "shell", "onix.sh")
		stdout, _, err := captureStdio(func() error {
			return sourceFromBashLike(snippetPath)
		})
		if err != nil {
			t.Fatalf("sourceFromBashLike: %v", err)
		}
		if !strings.Contains(stdout, "no .bashrc or .zshrc") {
			t.Errorf("expected 'no .bashrc or .zshrc' notice, got: %q", stdout)
		}
	})
}
