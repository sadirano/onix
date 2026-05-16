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
