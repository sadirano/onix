package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHome(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		got, err := resolveHome("/tmp/foo")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(filepath.ToSlash(got), "/tmp/foo") {
			t.Errorf("got %q, want suffix /tmp/foo", got)
		}
	})

	t.Run("default", func(t *testing.T) {
		got, err := resolveHome("")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(filepath.ToSlash(got), "/.onix") {
			t.Errorf("got %q, want suffix /.onix", got)
		}
	})
}

func TestStartsWithDash(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"-h", true},
		{"--help", true},
		{"alias", false},
		{"", false},
		{"-", true},
	}
	for _, tc := range tests {
		got := startsWithDash(tc.in)
		if got != tc.want {
			t.Errorf("startsWithDash(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPwshBin(t *testing.T) {
	bin := pwshBin()
	if bin == "" {
		t.Error("pwshBin returned empty string")
	}
}

