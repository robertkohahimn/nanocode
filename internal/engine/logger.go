package engine

import (
	"encoding/json"
	"io"
	"time"
)

// EngineLogger emits structured JSON-lines logs for engine decisions.
// If the writer is nil, all methods are no-ops (zero cost when logging is off).
type EngineLogger struct {
	SessionID string
	w         io.Writer
}

// NewEngineLogger creates a logger that writes to w. If w is nil, logging is a no-op.
func NewEngineLogger(sessionID string, w io.Writer) *EngineLogger {
	return &EngineLogger{SessionID: sessionID, w: w}
}

// logEntry is the common structure for all log entries.
// Fields with meaningful zero values (int, bool) omit omitempty so that
// values like iteration=0, is_error=false, etc. are always present.
// String fields that are legitimately optional keep omitempty.
type logEntry struct {
	Type             string `json:"type"`
	SessionID        string `json:"session_id"`
	Timestamp        int64  `json:"timestamp"`
	Iteration        int    `json:"iteration"`
	ToolCalls        int    `json:"tool_calls"`
	ToolName         string `json:"tool,omitempty"`
	DurationMs       int64  `json:"duration_ms,omitempty"`
	IsError          bool   `json:"is_error"`
	File             string `json:"file,omitempty"`
	EditCount        int    `json:"edit_count"`
	TotalMessages    int    `json:"total_messages"`
	WindowedMessages int    `json:"windowed_messages"`
	Iterations       int    `json:"iterations"`
	TotalDurationMs  int64  `json:"total_duration_ms,omitempty"`
}

func (l *EngineLogger) emit(entry logEntry) {
	if l == nil || l.w == nil {
		return
	}
	entry.SessionID = l.SessionID
	entry.Timestamp = time.Now().UnixMilli()
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')
	// Best-effort write; ignore errors to avoid disrupting the engine.
	_, _ = l.w.Write(data)
}

// LogIteration records the start of an engine loop iteration.
func (l *EngineLogger) LogIteration(iter int, toolCallCount int) {
	l.emit(logEntry{
		Type:      "iteration",
		Iteration: iter,
		ToolCalls: toolCallCount,
	})
}

// LogToolCall records a tool invocation with timing.
func (l *EngineLogger) LogToolCall(toolName string, duration time.Duration, isError bool) {
	l.emit(logEntry{
		Type:       "tool_call",
		ToolName:   toolName,
		DurationMs: duration.Milliseconds(),
		IsError:    isError,
	})
}

// LogDoomLoop records doom loop detection.
func (l *EngineLogger) LogDoomLoop(file string, editCount int) {
	l.emit(logEntry{
		Type:      "doom_loop",
		File:      file,
		EditCount: editCount,
	})
}

// LogContextWindow records context windowing decisions.
func (l *EngineLogger) LogContextWindow(totalMessages, windowedMessages int) {
	l.emit(logEntry{
		Type:             "context_window",
		TotalMessages:    totalMessages,
		WindowedMessages: windowedMessages,
	})
}

// LogSessionEnd records final session stats.
func (l *EngineLogger) LogSessionEnd(iterations int, totalDuration time.Duration) {
	l.emit(logEntry{
		Type:            "session_end",
		Iterations:      iterations,
		TotalDurationMs: totalDuration.Milliseconds(),
	})
}
