package visual

import "testing"

func TestFallback(t *testing.T) {
	tests := []struct {
		name       string
		value, def string
		want       string
	}{
		{"value returned when set", "vim", "nvim", "vim"},
		{"default when empty", "", "nvim", "nvim"},
		{"default when whitespace-only", "   ", "nvim", "nvim"},
		{"value returned even if def is empty", "vim", "", "vim"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallback(tt.value, tt.def); got != tt.want {
				t.Errorf("fallback(%q, %q) = %q, want %q", tt.value, tt.def, got, tt.want)
			}
		})
	}
}

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

func TestApplyDefaults(t *testing.T) {
	t.Run("zero config gets all defaults", func(t *testing.T) {
		cfg := Config{}
		cfg.ApplyDefaults()
		def := Default()

		if cfg.FZF.Destination.Prompt != def.FZF.Destination.Prompt {
			t.Errorf("Destination.Prompt: got %q, want %q", cfg.FZF.Destination.Prompt, def.FZF.Destination.Prompt)
		}
		if cfg.FZF.Destination.Layout != def.FZF.Destination.Layout {
			t.Errorf("Destination.Layout: got %q, want %q", cfg.FZF.Destination.Layout, def.FZF.Destination.Layout)
		}
		if cfg.FZF.Destination.Header != def.FZF.Destination.Header {
			t.Errorf("Destination.Header: got %q, want %q", cfg.FZF.Destination.Header, def.FZF.Destination.Header)
		}
	})
	t.Run("explicit values are preserved", func(t *testing.T) {
		cfg := Config{}
		cfg.FZF.Destination.Prompt = "go to> "
		cfg.ApplyDefaults()

		if cfg.FZF.Destination.Prompt != "go to> " {
			t.Errorf("Destination.Prompt: got %q, want \"go to> \"", cfg.FZF.Destination.Prompt)
		}
		if cfg.FZF.Destination.Layout == "" {
			t.Error("Destination.Layout should have been filled by ApplyDefaults")
		}
	})
}
