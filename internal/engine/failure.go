package engine

import (
	"context"
	"time"

	"github.com/robertkohahimn/nanocode/internal/store"
)

// FailureType categorizes the kind of failure.
type FailureType string

const (
	FailureDoomLoop  FailureType = "doom_loop"
	FailureToolError FailureType = "tool_error"
	FailureTimeout   FailureType = "timeout"
	FailureMaxIter   FailureType = "max_iterations"
)

// FailureCollector tracks tool usage and file edits during an engine run,
// and records failures to the store. All methods are nil-safe so the
// collector is a no-op when disabled.
type FailureCollector struct {
	store     store.Store
	sessionID string
	toolsUsed map[string]bool
	fileEdits map[string]bool
}

// NewFailureCollector creates a FailureCollector bound to the given session.
// Returns nil if store or sessionID is empty, making all methods no-ops.
func NewFailureCollector(st store.Store, sessionID string) *FailureCollector {
	if st == nil || sessionID == "" {
		return nil
	}
	return &FailureCollector{
		store:     st,
		sessionID: sessionID,
		toolsUsed: make(map[string]bool),
		fileEdits: make(map[string]bool),
	}
}

// TrackTool records that a tool was used during this run.
func (fc *FailureCollector) TrackTool(name string) {
	if fc == nil {
		return
	}
	fc.toolsUsed[name] = true
}

// TrackFile records that a file was edited during this run.
func (fc *FailureCollector) TrackFile(path string) {
	if fc == nil {
		return
	}
	fc.fileEdits[path] = true
}

// Record persists a failure event to the store.
func (fc *FailureCollector) Record(ctx context.Context, failType FailureType, desc string, iterations int) {
	if fc == nil {
		return
	}

	tools := make([]string, 0, len(fc.toolsUsed))
	for t := range fc.toolsUsed {
		tools = append(tools, t)
	}
	files := make([]string, 0, len(fc.fileEdits))
	for f := range fc.fileEdits {
		files = append(files, f)
	}

	rec := &store.FailureRecord{
		SessionID:    fc.sessionID,
		Timestamp:    time.Now().Unix(),
		FailureType:  string(failType),
		Description:  desc,
		ToolsUsed:    store.MarshalStringSlice(tools),
		FilesTouched: store.MarshalStringSlice(files),
		Iterations:   iterations,
	}

	// Best-effort: log errors but don't fail the engine run.
	_ = fc.store.CreateFailure(ctx, rec)
}
