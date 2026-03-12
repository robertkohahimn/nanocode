package engine

import (
	"testing"
)

func TestParseSelection_All(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"Y", 3},
		{"y", 3},
		{"", 3},
		{"yes", 3},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSelection(tt.input, tt.count)
			if err != nil {
				t.Fatalf("parseSelection(%q, %d): %v", tt.input, tt.count, err)
			}
			if len(result) != tt.count {
				t.Errorf("expected %d selections, got %d", tt.count, len(result))
			}
			for i := 0; i < tt.count; i++ {
				if !result[i] {
					t.Errorf("expected index %d to be selected", i)
				}
			}
		})
	}
}

func TestParseSelection_None(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"N"},
		{"n"},
		{"no"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSelection(tt.input, 3)
			if err != nil {
				t.Fatalf("parseSelection(%q, 3): %v", tt.input, err)
			}
			for i, selected := range result {
				if selected {
					t.Errorf("expected index %d to NOT be selected", i)
				}
			}
		})
	}
}
