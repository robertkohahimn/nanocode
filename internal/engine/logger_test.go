package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLogIteration(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-1", &buf)
	l.LogIteration(3, 2)

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}
	if entry.Type != "iteration" {
		t.Errorf("type = %q, want %q", entry.Type, "iteration")
	}
	if entry.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want %q", entry.SessionID, "sess-1")
	}
	if entry.Iteration != 3 {
		t.Errorf("iteration = %d, want 3", entry.Iteration)
	}
	if entry.ToolCalls != 2 {
		t.Errorf("tool_calls = %d, want 2", entry.ToolCalls)
	}
	if entry.Timestamp == 0 {
		t.Error("timestamp should be nonzero")
	}
}

func TestLogToolCall(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-2", &buf)
	l.LogToolCall("read", 45*time.Millisecond, false)

	raw := buf.String()
	// Verify is_error:false is always present (not omitted)
	if !strings.Contains(raw, `"is_error":false`) {
		t.Errorf("expected is_error:false in output, got: %s", raw)
	}

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.Type != "tool_call" {
		t.Errorf("type = %q, want %q", entry.Type, "tool_call")
	}
	if entry.ToolName != "read" {
		t.Errorf("tool = %q, want %q", entry.ToolName, "read")
	}
	if entry.DurationMs != 45 {
		t.Errorf("duration_ms = %d, want 45", entry.DurationMs)
	}
	if entry.IsError {
		t.Error("is_error should be false")
	}
}

func TestLogToolCallError(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-err", &buf)
	l.LogToolCall("bash", 100*time.Millisecond, true)

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !entry.IsError {
		t.Error("is_error should be true")
	}
}

func TestLogDoomLoop(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-3", &buf)
	l.LogDoomLoop("/path/to/file.go", 6)

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.Type != "doom_loop" {
		t.Errorf("type = %q, want %q", entry.Type, "doom_loop")
	}
	if entry.File != "/path/to/file.go" {
		t.Errorf("file = %q, want %q", entry.File, "/path/to/file.go")
	}
	if entry.EditCount != 6 {
		t.Errorf("edit_count = %d, want 6", entry.EditCount)
	}
}

func TestLogContextWindow(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-4", &buf)
	l.LogContextWindow(60, 40)

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.Type != "context_window" {
		t.Errorf("type = %q, want %q", entry.Type, "context_window")
	}
	if entry.TotalMessages != 60 {
		t.Errorf("total_messages = %d, want 60", entry.TotalMessages)
	}
	if entry.WindowedMessages != 40 {
		t.Errorf("windowed_messages = %d, want 40", entry.WindowedMessages)
	}
}

func TestLogSessionEnd(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-5", &buf)
	l.LogSessionEnd(10, 5*time.Second)

	var entry logEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.Type != "session_end" {
		t.Errorf("type = %q, want %q", entry.Type, "session_end")
	}
	if entry.Iterations != 10 {
		t.Errorf("iterations = %d, want 10", entry.Iterations)
	}
	if entry.TotalDurationMs != 5000 {
		t.Errorf("total_duration_ms = %d, want 5000", entry.TotalDurationMs)
	}
}

func TestLogNilWriter(t *testing.T) {
	l := NewEngineLogger("sess-nil", nil)
	// None of these should panic
	l.LogIteration(1, 0)
	l.LogToolCall("read", time.Second, false)
	l.LogDoomLoop("file.go", 3)
	l.LogContextWindow(10, 5)
	l.LogSessionEnd(1, time.Second)
}

func TestLogNilLogger(t *testing.T) {
	var l *EngineLogger
	// A nil logger should not panic
	l.emit(logEntry{Type: "test"})
}

func TestLogMultipleEntries(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-multi", &buf)
	l.LogIteration(1, 2)
	l.LogToolCall("read", time.Millisecond, false)
	l.LogToolCall("bash", 2*time.Millisecond, true)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), buf.String())
	}
	for i, line := range lines {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}
