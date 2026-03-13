package tool

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// --- BashTool tests ---

func TestBashToolBasic(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected hello, got %q", result)
	}
}

func TestBashToolNonZeroExit(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"exit 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Exit code 1") {
		t.Errorf("expected exit code 1, got %q", result)
	}
}

func TestBashToolRejected(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return false }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "rejected") {
		t.Errorf("expected rejected, got %q", result)
	}
}

func TestBashToolTimeout(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"sleep 60","timeout":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "timed out") {
		t.Errorf("expected timeout, got %q", result)
	}
}

// --- Command feedback tests ---

func TestExtractFirstLine(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		maxLen   int
		expected string
	}{
		{"empty", "", 60, ""},
		{"single line", "hello world", 60, "hello world"},
		{"multiline", "first line\nsecond line\nthird", 60, "first line"},
		{"long line truncated", "this is a very long line that should be truncated", 20, "this is a very long "},
		{"multiline with long first", "this is a very long first line\nsecond", 20, "this is a very long "},
		{"whitespace only", "   \n\n", 60, ""},
		{"leading whitespace", "  trimmed line\nnext", 60, "trimmed line"},
		{"utf8 truncation", "日本語テキスト", 5, "日本語テキ"},
		{"utf8 emoji truncation", "Hello 👋🌍🎉 World", 10, "Hello 👋🌍🎉 "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstLine(tt.output, tt.maxLen)
			if got != tt.expected {
				t.Errorf("extractFirstLine(%q, %d) = %q, want %q", tt.output, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestFormatCommandFeedback(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		exitCode int
		timedOut bool
		wantIcon string
		wantText string
	}{
		{
			name:     "success with output",
			output:   "/Users/demo/project",
			exitCode: 0,
			timedOut: false,
			wantIcon: "✓",
			wantText: "/Users/demo/project",
		},
		{
			name:     "success empty output",
			output:   "",
			exitCode: 0,
			timedOut: false,
			wantIcon: "✓",
			wantText: "",
		},
		{
			name:     "failure with output",
			output:   "command not found",
			exitCode: 127,
			timedOut: false,
			wantIcon: "✗",
			wantText: "exit 127",
		},
		{
			name:     "timeout with output",
			output:   "partial output",
			exitCode: -1,
			timedOut: true,
			wantIcon: "⏱",
			wantText: "timed out: partial output",
		},
		{
			name:     "timeout empty output",
			output:   "",
			exitCode: -1,
			timedOut: true,
			wantIcon: "⏱",
			wantText: "timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCommandFeedback(tt.output, tt.exitCode, tt.timedOut)
			if !strings.Contains(got, tt.wantIcon) {
				t.Errorf("formatCommandFeedback() missing icon %q, got %q", tt.wantIcon, got)
			}
			if tt.wantText != "" && !strings.Contains(got, tt.wantText) {
				t.Errorf("formatCommandFeedback() missing text %q, got %q", tt.wantText, got)
			}
		})
	}
}

func TestBashToolFeedbackIntegration(t *testing.T) {
	// Capture stderr to verify feedback is printed
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	_, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}

	w.Close()
	os.Stderr = oldStderr

	var buf [1024]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	// Should contain success icon and output preview
	if !strings.Contains(output, "✓") {
		t.Errorf("expected success icon in stderr, got %q", output)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello' in feedback, got %q", output)
	}
}
