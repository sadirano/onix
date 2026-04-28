package installer

import "testing"

func TestNormalizeRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sadirano/onix-sg", "sadirano/onix-sg"},
		{"github.com/sadirano/onix-sg", "sadirano/onix-sg"},
		{"https://github.com/sadirano/onix-sg", "sadirano/onix-sg"},
		{"http://github.com/sadirano/onix-sg", "sadirano/onix-sg"},
		{"vendor/repo", "vendor/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeRepo(tt.input); got != tt.want {
				t.Errorf("normalizeRepo(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
