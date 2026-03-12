package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/tool"
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
		{"-1", 3},     // negative number (not a valid range)
		{"1--3", 3},   // double dash
		{"-", 3},      // just a dash
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

func TestCollectBashConfirmations_MultipleBash(t *testing.T) {
	// Create bash tool
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)
	bashTool := tool.NewBashTool(reader)
	bashTool.SetToolCallIDGetter(func(ctx context.Context) string {
		return ToolCallIDFromContext(ctx)
	})

	// Create tool calls with multiple bash commands
	toolCalls := []*provider.ToolCall{
		{
			ID:    "tc1",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "mkdir -p test"}`),
		},
		{
			ID:    "tc2",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "pwd"}`),
		},
		{
			ID:    "tc3",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "ls"}`),
		},
	}

	var output bytes.Buffer
	err := collectBashConfirmations(toolCalls, bashTool, nil, reader, &output)
	if err != nil {
		t.Fatalf("collectBashConfirmations: %v", err)
	}

	// Verify output contains the batch prompt
	outStr := output.String()
	if !strings.Contains(outStr, "mkdir -p test") {
		t.Errorf("output should contain command, got: %s", outStr)
	}

	// Test that overrides were set by executing through the tool
	// We can't directly check the map, but we can verify behavior
	ctx := context.WithValue(context.Background(), toolCallIDKey{}, "tc1")
	result, err := bashTool.Execute(ctx, json.RawMessage(`{"command": "echo hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should be approved (from the batch "Y" response)
	if strings.Contains(result, "rejected") {
		t.Errorf("command should be approved, got: %s", result)
	}
}

func TestCollectBashConfirmations_SingleBash(t *testing.T) {
	input := bytes.NewBufferString("")
	reader := bufio.NewReader(input)
	bashTool := tool.NewBashTool(reader)

	// Single bash command should return nil (let normal flow handle it)
	toolCalls := []*provider.ToolCall{
		{
			ID:    "tc1",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "pwd"}`),
		},
	}

	var output bytes.Buffer
	err := collectBashConfirmations(toolCalls, bashTool, nil, reader, &output)
	if err != nil {
		t.Fatalf("collectBashConfirmations: %v", err)
	}

	// Should not have prompted (empty output)
	if output.Len() > 0 {
		t.Errorf("single bash should not trigger batch prompt, got: %s", output.String())
	}
}

func TestCollectBashConfirmations_NoBash(t *testing.T) {
	input := bytes.NewBufferString("")
	reader := bufio.NewReader(input)
	bashTool := tool.NewBashTool(reader)

	// No bash commands should return nil
	toolCalls := []*provider.ToolCall{
		{
			ID:    "tc1",
			Name:  "read",
			Input: json.RawMessage(`{"file_path": "/tmp/test.txt"}`),
		},
		{
			ID:    "tc2",
			Name:  "write",
			Input: json.RawMessage(`{"file_path": "/tmp/test.txt", "content": "hello"}`),
		},
	}

	var output bytes.Buffer
	err := collectBashConfirmations(toolCalls, bashTool, nil, reader, &output)
	if err != nil {
		t.Fatalf("collectBashConfirmations: %v", err)
	}

	// Should not have prompted (empty output)
	if output.Len() > 0 {
		t.Errorf("no bash should not trigger batch prompt, got: %s", output.String())
	}
}

func TestCollectBashConfirmations_MixedTools(t *testing.T) {
	// Mix of bash and non-bash tools
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)
	bashTool := tool.NewBashTool(reader)

	toolCalls := []*provider.ToolCall{
		{
			ID:    "tc1",
			Name:  "read",
			Input: json.RawMessage(`{"file_path": "/tmp/test.txt"}`),
		},
		{
			ID:    "tc2",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "pwd"}`),
		},
		{
			ID:    "tc3",
			Name:  "bash",
			Input: json.RawMessage(`{"command": "ls"}`),
		},
	}

	var output bytes.Buffer
	err := collectBashConfirmations(toolCalls, bashTool, nil, reader, &output)
	if err != nil {
		t.Fatalf("collectBashConfirmations: %v", err)
	}

	// Should have prompted (2 bash commands)
	outStr := output.String()
	if !strings.Contains(outStr, "pwd") || !strings.Contains(outStr, "ls") {
		t.Errorf("output should contain bash commands, got: %s", outStr)
	}
}
