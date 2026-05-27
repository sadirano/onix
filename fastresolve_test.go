package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestFastListNames_Alphabetical(t *testing.T) {
	home := t.TempDir()

	// Register three aliases in non-alphabetical order.
	_ = (&AddCmd{Alias: "cherry", Path: "C:/cherry"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	_ = (&AddCmd{Alias: "apple", Path: "C:/apple"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})
	_ = (&AddCmd{Alias: "banana", Path: "C:/banana"}).Run(context.Background(), &env{Home: home, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin})

	stdout, _, _ := captureStdio(func() error {
		return fastListNames(home, os.Stdout)
	})
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if lines[0] != "apple" || lines[1] != "banana" || lines[2] != "cherry" {
		t.Errorf("alphabetical sort failed: %v", lines)
	}
}
