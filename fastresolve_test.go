package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sadirano/onix/internal/store"
)

func TestFastListNames_Frecency(t *testing.T) {
	home := t.TempDir()
	
	// Register three aliases.
	(&AddCmd{Alias: "apple", Path: "C:/apple"}).Run(context.Background(), &env{Home: home})
	(&AddCmd{Alias: "banana", Path: "C:/banana"}).Run(context.Background(), &env{Home: home})
	(&AddCmd{Alias: "cherry", Path: "C:/cherry"}).Run(context.Background(), &env{Home: home})

	// Initial sort should be alphabetical: apple, banana, cherry.
	stdout, _, _ := captureStdio(func() error {
		return fastListNames(home)
	})
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if lines[0] != "apple" || lines[1] != "banana" || lines[2] != "cherry" {
		t.Errorf("initial sort failed: %v", lines)
	}

	// Record usage: banana (most), cherry (second), apple (none).
	store.RecordUsage(home, "banana")
	store.RecordUsage(home, "banana")
	store.RecordUsage(home, "cherry")

	// New sort should be: banana, cherry, apple.
	stdout, _, _ = captureStdio(func() error {
		return fastListNames(home)
	})
	lines = strings.Split(strings.TrimSpace(stdout), "\n")
	if lines[0] != "banana" || lines[1] != "cherry" || lines[2] != "apple" {
		t.Errorf("frecency sort failed: %v", lines)
	}
}
