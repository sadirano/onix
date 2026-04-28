package opener

import "testing"

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
