package main

import (
	"reflect"
	"testing"
)

func TestClassifyEditor(t *testing.T) {
	cases := map[string]editorFamily{
		"nvim":                      familyPlus,
		"vim":                       familyPlus,
		"/usr/bin/nvim":             familyPlus,
		"code":                      familyGoto,
		"code.exe":                  familyGoto,
		`C:\Program Files\code.exe`: familyGoto,
		"Code - Insiders":           familyPlus, // not a binary name we know
		"code-insiders":             familyGoto,
		"cursor":                    familyGoto,
		"windsurf":                  familyGoto,
		"codium":                    familyGoto,
		"":                          familyPlus,
	}
	for in, want := range cases {
		if got := classifyEditor(in); got != want {
			t.Errorf("classifyEditor(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestEditorArgs(t *testing.T) {
	targets := []editTarget{
		{file: "main.go", line: "42"},
		{file: "search.go", line: "7"},
	}
	noLine := []editTarget{{file: "main.go"}, {file: "README.md"}}

	cases := []struct {
		name    string
		editor  string
		targets []editTarget
		want    []string
	}{
		{"vim with lines", "nvim", targets, []string{"+42", "main.go", "+7", "search.go"}},
		{"code with lines", "code", targets, []string{"--goto", "main.go:42", "--goto", "search.go:7"}},
		{"vim no lines", "nvim", noLine, []string{"main.go", "README.md"}},
		{"code no lines", "code", noLine, []string{"main.go", "README.md"}},
	}
	for _, c := range cases {
		if got := editorArgs(c.editor, c.targets); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: editorArgs(%q) = %v, want %v", c.name, c.editor, got, c.want)
		}
	}
}
