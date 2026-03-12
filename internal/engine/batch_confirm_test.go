package engine

import (
	"bufio"
	"bytes"
	"strings"
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

func TestParseSelection_Numbers(t *testing.T) {
	tests := []struct {
		input    string
		count    int
		expected []bool
	}{
		{"1", 3, []bool{true, false, false}},
		{"1,3", 3, []bool{true, false, true}},
		{"2,3", 3, []bool{false, true, true}},
		{"1,2,3", 3, []bool{true, true, true}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSelection(tt.input, tt.count)
			if err != nil {
				t.Fatalf("parseSelection(%q, %d): %v", tt.input, tt.count, err)
			}
			for i, want := range tt.expected {
				if result[i] != want {
					t.Errorf("index %d: got %v, want %v", i, result[i], want)
				}
			}
		})
	}
}

func TestParseSelection_Spaces(t *testing.T) {
	result, err := parseSelection("1 3", 4)
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	expected := []bool{true, false, true, false}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("index %d: got %v, want %v", i, result[i], want)
		}
	}
}

func TestParseSelection_Range(t *testing.T) {
	result, err := parseSelection("1-3", 4)
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	expected := []bool{true, true, true, false}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("index %d: got %v, want %v", i, result[i], want)
		}
	}
}

func TestParseSelection_Invalid(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"abc", 3},
		{"0", 3},      // out of range (1-based)
		{"4", 3},      // out of range
		{"1-5", 3},    // range exceeds count
		{"3-1", 3},    // inverted range
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseSelection(tt.input, tt.count)
			if err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
		})
	}
}

func TestPromptBatch_ApproveAll(t *testing.T) {
	// Simulate user typing "Y\n"
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)

	var output bytes.Buffer

	commands := []pendingCommand{
		{toolCallID: "tc1", command: "mkdir -p project"},
		{toolCallID: "tc2", command: "pwd"},
		{toolCallID: "tc3", command: "ls"},
	}

	decisions, err := promptBatch(commands, reader, &output)
	if err != nil {
		t.Fatalf("promptBatch: %v", err)
	}

	// All should be approved
	for _, cmd := range commands {
		dec, ok := decisions[cmd.toolCallID]
		if !ok {
			t.Errorf("missing decision for %s", cmd.toolCallID)
			continue
		}
		if !dec.approved {
			t.Errorf("expected %s to be approved", cmd.toolCallID)
		}
		if dec.skipped {
			t.Errorf("expected %s to not be skipped", cmd.toolCallID)
		}
	}

	// Check output contains the commands
	outStr := output.String()
	if !strings.Contains(outStr, "mkdir -p project") {
		t.Errorf("output should contain command, got: %s", outStr)
	}
}

func TestPromptBatch_PartialSelection(t *testing.T) {
	input := bytes.NewBufferString("1,3\n")
	reader := bufio.NewReader(input)
	var output bytes.Buffer

	commands := []pendingCommand{
		{toolCallID: "tc1", command: "mkdir"},
		{toolCallID: "tc2", command: "pwd"},
		{toolCallID: "tc3", command: "ls"},
	}

	decisions, err := promptBatch(commands, reader, &output)
	if err != nil {
		t.Fatalf("promptBatch: %v", err)
	}

	// tc1 and tc3 approved, tc2 skipped
	if !decisions["tc1"].approved {
		t.Error("tc1 should be approved")
	}
	if decisions["tc2"].approved {
		t.Error("tc2 should NOT be approved")
	}
	if !decisions["tc2"].skipped {
		t.Error("tc2 should be marked as skipped")
	}
	if !decisions["tc3"].approved {
		t.Error("tc3 should be approved")
	}
}

func TestPromptBatch_RejectAll(t *testing.T) {
	input := bytes.NewBufferString("n\n")
	reader := bufio.NewReader(input)
	var output bytes.Buffer

	commands := []pendingCommand{
		{toolCallID: "tc1", command: "mkdir"},
		{toolCallID: "tc2", command: "pwd"},
	}

	decisions, err := promptBatch(commands, reader, &output)
	if err != nil {
		t.Fatalf("promptBatch: %v", err)
	}

	for _, cmd := range commands {
		dec := decisions[cmd.toolCallID]
		if dec.approved {
			t.Errorf("%s should NOT be approved", cmd.toolCallID)
		}
		if dec.skipped {
			t.Errorf("%s should NOT be skipped (reject all, not partial)", cmd.toolCallID)
		}
	}
}
