package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestGrepCmd_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	err := (&GrepCmd{Args: []string{"acme", "query"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error when rg/fzf missing, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestFindCmd_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	target := t.TempDir()
	_ = (&AddCmd{Alias: "acme", Path: target}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	err := (&FindCmd{Args: []string{"acme", "query"}}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil {
		t.Fatal("expected error when fd/fzf missing, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestGrepCmd_TooFewArgs(t *testing.T) {
	err := (&GrepCmd{Args: nil}).Run(context.Background(), &env{Home: t.TempDir(), Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestFindCmd_TooFewArgs(t *testing.T) {
	err := (&FindCmd{Args: nil}).Run(context.Background(), &env{Home: t.TempDir(), Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestOpenSelectionsInEditor(t *testing.T) {
	// Set EDITOR to a command that does nothing
	t.Setenv("EDITOR", "true")

	err := openSelectionsInEditor(context.Background(), ".", []string{"file.go:10:text", "other.go"})
	if err != nil {
		t.Errorf("openSelectionsInEditor failed: %v", err)
	}
}
