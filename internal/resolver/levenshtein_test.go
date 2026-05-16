package resolver

import "testing"

func TestComputeDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "b", 1},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"gopher", "gofer", 2},
		{"✨star", "⭐star", 1}, // Unicode aware
		{"onix", "onix-sdk", 4},
	}

	for _, tt := range tests {
		if got := ComputeDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("ComputeDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
