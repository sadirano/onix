package visual

import "testing"

func TestAppendLayoutArg(t *testing.T) {
	base := []string{"--ansi", "--multi"}
	tests := []struct {
		name   string
		layout string
		want   []string
	}{
		{"empty layout is a no-op", "", base},
		{"default is a no-op", "default", base},
		{"DEFAULT is a no-op (case-insensitive)", "DEFAULT", base},
		{"reverse-list appends", "reverse-list", append(base, "--layout", "reverse-list")},
		{"whitespace is stripped", "  reverse-list  ", append(base, "--layout", "reverse-list")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppendLayoutArg(append([]string{}, base...), tt.layout)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.FZF.Destination.Prompt == "" {
		t.Error("FZF.Destination.Prompt should not be empty")
	}
	if cfg.FZF.Destination.Layout == "" {
		t.Error("FZF.Destination.Layout should not be empty")
	}
	if cfg.FZF.Destination.Header == "" {
		t.Error("FZF.Destination.Header should not be empty")
	}
}

func TestDefault_allFieldsSet(t *testing.T) {
	cfg := Default()
	d := cfg.FZF.Destination
	fields := map[string]string{
		"Prompt":        d.Prompt,
		"Layout":        d.Layout,
		"Preview":       d.Preview,
		"PreviewWindow": d.PreviewWindow,
		"Header":        d.Header,
		"Height":        d.Height,
	}
	for name, val := range fields {
		if val == "" {
			t.Errorf("Default().FZF.Destination.%s is empty", name)
		}
	}
}
