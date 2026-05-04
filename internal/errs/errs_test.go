package errs

import "testing"

func TestExitError_Error(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "exit status 0"},
		{1, "exit status 1"},
		{2, "exit status 2"},
		{127, "exit status 127"},
		{-1, "exit status -1"},
	}
	for _, tt := range tests {
		e := &ExitError{Code: tt.code}
		if got := e.Error(); got != tt.want {
			t.Errorf("ExitError{%d}.Error() = %q, want %q", tt.code, got, tt.want)
		}
	}
}
