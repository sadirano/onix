package dispatch

import "testing"

func TestIsCoreCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"nil args", nil, false},
		{"empty args", []string{}, false},
		{"install", []string{"install"}, true},
		{"update", []string{"update"}, true},
		{"list", []string{"list"}, true},
		{"init", []string{"init"}, true},
		{"help", []string{"help"}, true},
		{"-h", []string{"-h"}, true},
		{"--help", []string{"--help"}, true},
		{"-a", []string{"-a"}, true},
		{"--alias", []string{"--alias"}, true},
		{"case-insensitive INSTALL", []string{"INSTALL"}, true},
		{"alias name is not core", []string{"myproject"}, false},
		{"sg is not core", []string{"sg"}, false},
		{"ff is not core", []string{"ff"}, false},
		{"only first arg checked", []string{"install", "sg"}, true},
		{"only first arg checked (non-core)", []string{"myalias", "install"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCoreCommand(tt.args); got != tt.want {
				t.Errorf("IsCoreCommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
