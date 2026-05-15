package main

import (
	"strings"
	"testing"
)

func TestGrepCmd_NotFound(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	(&AddCmd{Alias: "acme", Path: target}).Run(&env{Home: home})

	err := (&GrepCmd{Args: []string{"acme", "query"}}).Run(&env{Home: home})
	if err == nil {
		t.Fatal("expected error when rg/fzf missing, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestFindCmd_NotFound(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	(&AddCmd{Alias: "acme", Path: target}).Run(&env{Home: home})

	err := (&FindCmd{Args: []string{"acme", "query"}}).Run(&env{Home: home})
	if err == nil {
		t.Fatal("expected error when fd/fzf missing, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestOpenSelectionsInEditor(t *testing.T) {
	// Set EDITOR to a command that does nothing
	t.Setenv("EDITOR", "true")

	err := openSelectionsInEditor(".", []string{"file.go:10:text", "other.go"})
	if err != nil {
		t.Errorf("openSelectionsInEditor failed: %v", err)
	}
}
