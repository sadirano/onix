package main

import (
	"reflect"
	"testing"
)

func TestParseActionArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantSubdir string
		wantExtras []string
	}{
		{"no args", nil, "", "", nil},
		{"editor open", []string{"-n"}, "n", "", nil},
		{"print path", []string{"-y"}, "y", "", nil},
		{"explorer", []string{"-e"}, "e", "", nil},
		{"file open", []string{"-f"}, "f", "", nil},
		{"run command", []string{"-r", "go build"}, "r", "", []string{"go build"}},
		{"sg search", []string{"-sg"}, "sg", "", nil},
		{"sga search", []string{"-sga"}, "sga", "", nil},
		{"ff find", []string{"-ff"}, "ff", "", nil},
		{"subdir short flag", []string{"-s", "cmd"}, "", "cmd", nil},
		{"subdir long flag", []string{"--subdir", "pkg/api"}, "", "pkg/api", nil},
		{"extras collected", []string{"foo.go", "bar.go"}, "", "", []string{"foo.go", "bar.go"}},
		{"action after extras", []string{"foo.go", "-n"}, "n", "", []string{"foo.go"}},
		{"subdir and action", []string{"-s", "pkg", "-n"}, "n", "pkg", nil},
		{"extras and subdir", []string{"foo.go", "-s", "sub"}, "", "sub", []string{"foo.go"}},
		{"last action flag wins", []string{"-n", "-y"}, "y", "", nil},
		{"subdir missing value is no-op", []string{"-s"}, "", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, subdir, extras := parseActionArgs(tt.args)
			if action != tt.wantAction {
				t.Errorf("action: got %q, want %q", action, tt.wantAction)
			}
			if subdir != tt.wantSubdir {
				t.Errorf("subdir: got %q, want %q", subdir, tt.wantSubdir)
			}
			if !reflect.DeepEqual(extras, tt.wantExtras) {
				t.Errorf("extras: got %v, want %v", extras, tt.wantExtras)
			}
		})
	}
}
