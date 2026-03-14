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

// baseEntry contains fields common to all log entry types.
type baseEntry struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Timestamp int64  `json:"timestamp"`
}

// iterationEntry records the start of an engine loop iteration.
type iterationEntry struct {
	baseEntry
	Iteration int `json:"iteration"`
	ToolCalls int `json:"tool_calls"`
}

// toolCallEntry records a tool invocation with timing.
type toolCallEntry struct {
	baseEntry
	ToolName   string `json:"tool"`
	DurationMs int64  `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
}

// doomLoopEntry records doom loop detection.
type doomLoopEntry struct {
	baseEntry
	File      string `json:"file"`
	EditCount int    `json:"edit_count"`
}

// contextWindowEntry records context windowing decisions.
type contextWindowEntry struct {
	baseEntry
	TotalMessages    int `json:"total_messages"`
	WindowedMessages int `json:"windowed_messages"`
}

// sessionEndEntry records final session stats.
type sessionEndEntry struct {
	baseEntry
	Iterations      int   `json:"iterations"`
	TotalDurationMs int64 `json:"total_duration_ms"`
}

// checkpointEntry records a checkpoint injection.
type checkpointEntry struct {
	baseEntry
	Iteration int    `json:"iteration"`
	Level     string `json:"level"`
}

// summarizationEntry records a context summarization event.
type summarizationEntry struct {
	baseEntry
	OriginalMessages int `json:"original_messages"`
	ResultMessages   int `json:"result_messages"`
}

// base returns a baseEntry populated with the entry type, session ID, and current timestamp.
func (l *EngineLogger) base(entryType string) baseEntry {
	return baseEntry{
		Type:      entryType,
		SessionID: l.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// emit marshals the entry to JSON and writes it as a single line.
func (l *EngineLogger) emit(entry any) {
	if l == nil || l.w == nil {
		return
	}
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
	if l == nil || l.w == nil {
		return
	}
	l.emit(iterationEntry{
		baseEntry: l.base("iteration"),
		Iteration: iter,
		ToolCalls: toolCallCount,
	})
}

// LogToolCall records a tool invocation with timing.
func (l *EngineLogger) LogToolCall(toolName string, duration time.Duration, isError bool) {
	if l == nil || l.w == nil {
		return
	}
	l.emit(toolCallEntry{
		baseEntry:  l.base("tool_call"),
		ToolName:   toolName,
		DurationMs: duration.Milliseconds(),
		IsError:    isError,
	})
}

// LogDoomLoop records doom loop detection.
func (l *EngineLogger) LogDoomLoop(file string, editCount int) {
	if l == nil || l.w == nil {
		return
	}
	l.emit(doomLoopEntry{
		baseEntry: l.base("doom_loop"),
		File:      file,
		EditCount: editCount,
	})
}

// LogContextWindow records context windowing decisions.
func (l *EngineLogger) LogContextWindow(totalMessages, windowedMessages int) {
	if l == nil || l.w == nil {
		return
	}
	l.emit(contextWindowEntry{
		baseEntry:        l.base("context_window"),
		TotalMessages:    totalMessages,
		WindowedMessages: windowedMessages,
	})
}

// LogSessionEnd records final session stats.
func (l *EngineLogger) LogSessionEnd(iterations int, totalDuration time.Duration) {
	if l == nil || l.w == nil {
		return
	}
	l.emit(sessionEndEntry{
		baseEntry:       l.base("session_end"),
		Iterations:      iterations,
		TotalDurationMs: totalDuration.Milliseconds(),
	})
}

// LogCheckpoint records a checkpoint injection.
func (l *EngineLogger) LogCheckpoint(iteration int, level string) {
	if l == nil || l.w == nil {
		return
	}
	l.emit(checkpointEntry{
		baseEntry: l.base("checkpoint"),
		Iteration: iteration,
		Level:     level,
	})
}

// LogSummarization records a context summarization event.
func (l *EngineLogger) LogSummarization(originalMessages, resultMessages int) {
	if l == nil || l.w == nil {
		return
	}
	l.emit(summarizationEntry{
		baseEntry:        l.base("summarization"),
		OriginalMessages: originalMessages,
		ResultMessages:   resultMessages,
	})
}
