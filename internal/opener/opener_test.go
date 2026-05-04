package opener

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("text file is not binary", func(t *testing.T) {
		p := filepath.Join(dir, "text.go")
		if err := os.WriteFile(p, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if IsBinaryFile(p) {
			t.Error("expected false for text file")
		}
	})
	t.Run("file with null byte is binary", func(t *testing.T) {
		p := filepath.Join(dir, "binary.bin")
		if err := os.WriteFile(p, []byte("hello\x00world"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !IsBinaryFile(p) {
			t.Error("expected true for file with null byte")
		}
	})
	t.Run("empty file is not binary", func(t *testing.T) {
		p := filepath.Join(dir, "empty.txt")
		if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		if IsBinaryFile(p) {
			t.Error("expected false for empty file")
		}
	})
	t.Run("nonexistent file is not binary", func(t *testing.T) {
		if IsBinaryFile(filepath.Join(dir, "nosuchfile")) {
			t.Error("expected false for nonexistent file")
		}
	})
}

func TestEditorBase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"code", "code"},
		{"code.exe", "code"},
		{"CODE.EXE", "code"},
		{"nvim", "nvim"},
		{"nvim.exe", "nvim"},
		{"/usr/bin/nvim", "nvim"},
		{`C:\tools\vim.exe`, "vim"},
		{"code-insiders.exe", "code-insiders"},
		{"  nvim  ", "nvim"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := EditorBase(tt.input); got != tt.want {
				t.Errorf("EditorBase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
