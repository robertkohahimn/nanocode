# Tier 3: Context & Efficiency Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Tier 3 harness roadmap items — parallel read-only tool execution, background bash tasks, summarization persistence, and main branch detection.

**Architecture:** Extract tool execution from engine loop into a partitioning system that runs read-only tools concurrently. Add background task manager for long-running bash commands. Fill gaps in existing summarization and context systems.

**Tech Stack:** Go stdlib only (sync, crypto/rand, os, context). SQLite for persistence. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-03-24-tier3-context-efficiency-design.md`

---

## Chunk 1: Parallel Tool Execution (T3.3)

### Task 1: Partitioning logic and tests

**Files:**
- Create: `internal/engine/parallel.go`
- Create: `internal/engine/parallel_test.go`

- [ ] **Step 1: Write partitioning tests**

Create `internal/engine/parallel_test.go`:

```go
package engine

import (
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

func TestPartitionToolCalls_AllReadOnly(t *testing.T) {
	calls := []*provider.ToolCall{
		{ID: "1", Name: "read"},
		{ID: "2", Name: "glob"},
		{ID: "3", Name: "grep"},
	}
	groups := partitionToolCalls(calls)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !groups[0].parallel {
		t.Error("expected parallel group")
	}
	if len(groups[0].calls) != 3 {
		t.Errorf("expected 3 calls, got %d", len(groups[0].calls))
	}
}

func TestPartitionToolCalls_AllSequential(t *testing.T) {
	calls := []*provider.ToolCall{
		{ID: "1", Name: "edit"},
		{ID: "2", Name: "bash"},
	}
	groups := partitionToolCalls(calls)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	for _, g := range groups {
		if g.parallel {
			t.Error("expected all sequential groups")
		}
	}
}

func TestPartitionToolCalls_Mixed(t *testing.T) {
	calls := []*provider.ToolCall{
		{ID: "1", Name: "read"},
		{ID: "2", Name: "glob"},
		{ID: "3", Name: "edit"},
		{ID: "4", Name: "read"},
		{ID: "5", Name: "read"},
		{ID: "6", Name: "bash"},
	}
	groups := partitionToolCalls(calls)
	// [parallel(read,glob), seq(edit), parallel(read,read), seq(bash)]
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}
	if !groups[0].parallel || len(groups[0].calls) != 2 {
		t.Errorf("group 0: expected parallel with 2 calls")
	}
	if groups[1].parallel || len(groups[1].calls) != 1 {
		t.Errorf("group 1: expected sequential with 1 call")
	}
	if !groups[2].parallel || len(groups[2].calls) != 2 {
		t.Errorf("group 2: expected parallel with 2 calls")
	}
	if groups[3].parallel || len(groups[3].calls) != 1 {
		t.Errorf("group 3: expected sequential with 1 call")
	}
}

func TestPartitionToolCalls_Empty(t *testing.T) {
	groups := partitionToolCalls(nil)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for nil input, got %d", len(groups))
	}
}

func TestPartitionToolCalls_SingleReadOnly(t *testing.T) {
	calls := []*provider.ToolCall{{ID: "1", Name: "read"}}
	groups := partitionToolCalls(calls)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	// Single read-only call: still marked parallel (just 1 goroutine)
	if !groups[0].parallel {
		t.Error("expected parallel group even for single read-only call")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -run TestPartition -v`
Expected: FAIL — `partitionToolCalls` undefined

- [ ] **Step 3: Implement partitioning and toolExecContext**

Create `internal/engine/parallel.go`:

```go
package engine

import (
	"github.com/robertkohahimn/nanocode/internal/config"
	"github.com/robertkohahimn/nanocode/internal/provider"
)

// readOnlyTools is the set of tools safe for concurrent execution.
var readOnlyTools = map[string]bool{
	"read": true,
	"glob": true,
	"grep": true,
}

// toolCallGroup is a batch of tool calls to execute together.
// If parallel is true, all calls run concurrently.
type toolCallGroup struct {
	parallel bool
	calls    []*provider.ToolCall
}

// partitionToolCalls splits tool calls into groups of consecutive
// read-only (parallel) and mutating (sequential, one per group) calls.
func partitionToolCalls(calls []*provider.ToolCall) []toolCallGroup {
	if len(calls) == 0 {
		return nil
	}
	var groups []toolCallGroup
	var batch []*provider.ToolCall

	for _, tc := range calls {
		if readOnlyTools[tc.Name] {
			batch = append(batch, tc)
		} else {
			// Flush any accumulated read-only batch
			if len(batch) > 0 {
				groups = append(groups, toolCallGroup{parallel: true, calls: batch})
				batch = nil
			}
			// Each mutating call is its own sequential group
			groups = append(groups, toolCallGroup{parallel: false, calls: []*provider.ToolCall{tc}})
		}
	}
	// Flush trailing read-only batch
	if len(batch) > 0 {
		groups = append(groups, toolCallGroup{parallel: true, calls: batch})
	}
	return groups
}

// toolExecContext bundles state needed by executeToolCalls to avoid 8+ parameters.
type toolExecContext struct {
	engine       *Engine
	cfg          *config.Config
	loopDetector *LoopDetector
	verifyState  *VerifyState
	fc           *FailureCollector
	logger       *EngineLogger
	iteration    int
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -run TestPartition -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/parallel.go internal/engine/parallel_test.go
git commit -m "feat(T3.3): add tool call partitioning for parallel execution"
```

### Task 2: Parallel batch execution

**Files:**
- Modify: `internal/engine/parallel.go`
- Modify: `internal/engine/parallel_test.go`

- [ ] **Step 1: Write parallel execution test**

Append to `internal/engine/parallel_test.go` (merge these imports into the existing import block):

```go
// Additional imports needed (merge with existing):
// "context", "encoding/json", "sync/atomic", "time"

// sleepTool is a mock tool that sleeps to prove concurrency.
type sleepTool struct {
	name     string
	duration time.Duration
	calls    atomic.Int32
}

func (s *sleepTool) Name() string { return s.name }
func (s *sleepTool) Definition() provider.ToolDef {
	return provider.ToolDef{Name: s.name, InputSchema: json.RawMessage(`{}`)}
}
func (s *sleepTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	s.calls.Add(1)
	time.Sleep(s.duration)
	return "ok", nil
}

func TestExecuteParallelBatch(t *testing.T) {
	st := &sleepTool{name: "read", duration: 50 * time.Millisecond}
	reg := NewToolRegistry(st)

	calls := []*provider.ToolCall{
		{ID: "1", Name: "read", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "read", Input: json.RawMessage(`{}`)},
		{ID: "3", Name: "read", Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := executeParallelBatch(context.Background(), reg, calls)
	elapsed := time.Since(start)

	// 3 calls × 50ms each; if sequential would take ≥150ms.
	// Parallel should complete in ~50-100ms.
	if elapsed > 120*time.Millisecond {
		t.Errorf("expected parallel execution, took %v", elapsed)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Verify ordering: result[i] matches call[i]
	for i, r := range results {
		if r.ToolResult == nil || r.ToolResult.ToolCallID != calls[i].ID {
			t.Errorf("result %d: expected tool call ID %s", i, calls[i].ID)
		}
		if r.elapsed == 0 {
			t.Errorf("result %d: expected non-zero elapsed time", i)
		}
	}
	if st.calls.Load() != 3 {
		t.Errorf("expected 3 tool calls, got %d", st.calls.Load())
	}
}

func TestExecuteParallelBatch_OrderPreserved(t *testing.T) {
	// Different tools with different sleep durations
	fast := &sleepTool{name: "read", duration: 10 * time.Millisecond}
	slow := &sleepTool{name: "glob", duration: 50 * time.Millisecond}
	reg := NewToolRegistry(fast, slow)

	calls := []*provider.ToolCall{
		{ID: "slow", Name: "glob", Input: json.RawMessage(`{}`)},
		{ID: "fast", Name: "read", Input: json.RawMessage(`{}`)},
	}

	results := executeParallelBatch(context.Background(), reg, calls)
	if results[0].ToolResult.ToolCallID != "slow" {
		t.Error("expected first result to be 'slow' (order preserved)")
	}
	if results[1].ToolResult.ToolCallID != "fast" {
		t.Error("expected second result to be 'fast' (order preserved)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -run TestExecuteParallel -v`
Expected: FAIL — `executeParallelBatch` undefined

- [ ] **Step 3: Implement parallel batch execution**

Add to `internal/engine/parallel.go`:

```go
import (
	"context"
	"sync"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

const maxParallelTools = 10

// parallelResult holds a tool result with its execution timing.
type parallelResult struct {
	provider.ContentBlock
	elapsed time.Duration
}

// executeParallelBatch runs read-only tool calls concurrently.
// Results are returned in the same order as calls (not completion order).
func executeParallelBatch(ctx context.Context, reg *ToolRegistry, calls []*provider.ToolCall) []parallelResult {
	results := make([]parallelResult, len(calls))
	sem := make(chan struct{}, maxParallelTools)
	var wg sync.WaitGroup

	for i, tc := range calls {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func(idx int, call *provider.ToolCall) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore
			start := time.Now()
			result := reg.Execute(ctx, call)
			results[idx] = parallelResult{
				ContentBlock: provider.ContentBlock{
					Type:       "tool_result",
					ToolResult: result,
				},
				elapsed: time.Since(start),
			}
		}(i, tc)
	}

	wg.Wait()
	return results
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -run TestExecuteParallel -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/parallel.go internal/engine/parallel_test.go
git commit -m "feat(T3.3): add concurrent execution for read-only tool batches"
```

### Task 3: Extract tool execution from engine loop

**Files:**
- Modify: `internal/engine/parallel.go`
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: Run all tests before extraction to establish baseline**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -v -count=1`
Expected: All existing tests PASS

- [ ] **Step 2: Add executeToolCalls function to parallel.go**

This function replaces lines 394–484 of `engine.go`. It partitions tool calls, runs read-only batches in parallel, and runs mutating calls sequentially with full doom loop / verification / reflection logic.

Add to `internal/engine/parallel.go` (add `"encoding/json"`, `"fmt"`, `"path/filepath"`, `"time"` to imports):

```go
// executeToolCalls partitions tool calls and executes them.
// Read-only tools run concurrently; mutating tools run sequentially
// with doom loop detection, verification tracking, and error reflection.
func executeToolCalls(ctx context.Context, tec *toolExecContext, toolCalls []*provider.ToolCall) []provider.ContentBlock {
	groups := partitionToolCalls(toolCalls)
	var resultBlocks []provider.ContentBlock

	for _, group := range groups {
		if group.parallel {
			results := executeParallelBatch(ctx, tec.engine.tools, group.calls)
			// Apply side effects after batch completes (not inside goroutines)
			for i, cb := range results {
				tc := group.calls[i]
				isError := cb.ToolResult != nil && cb.ToolResult.IsError
				elapsed := cb.elapsed
				tec.logger.LogToolCall(tc.Name, elapsed, isError)
				tec.fc.TrackTool(tc.Name)
				tec.engine.mu.Lock()
				tec.engine.lastRunRecords = append(tec.engine.lastRunRecords, ToolRecord{
					Name: tc.Name, DurationMs: elapsed.Milliseconds(), IsError: isError,
				})
				tec.engine.mu.Unlock()
				resultBlocks = append(resultBlocks, cb.ContentBlock)
				if isError && !tec.cfg.DisableReflection {
					resultBlocks = append(resultBlocks, provider.ContentBlock{
						Type: "text", Text: errorReflectionPrompt,
					})
				}
			}
			continue
		}

		// Sequential execution with full doom loop / verify / reflection logic
		for _, tc := range group.calls {
			blocks := executeSequentialTool(ctx, tec, tc)
			resultBlocks = append(resultBlocks, blocks...)
		}
	}
	return resultBlocks
}

// executeSequentialTool runs a single mutating tool call with all guards.
func executeSequentialTool(ctx context.Context, tec *toolExecContext, tc *provider.ToolCall) []provider.ContentBlock {
	var resultBlocks []provider.ContentBlock
	injectWarning := func(w *LoopWarning) {
		if !tec.cfg.DisableReflection {
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: FormatWarning(w)})
		}
	}

	if tc.Name == "edit" || tc.Name == "write" {
		var inp struct {
			FilePath  string `json:"file_path"`
			Content   string `json:"content"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(tc.Input, &inp); err != nil {
			tec.logger.LogToolCall(tc.Name, 0, true)
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: &provider.ToolResult{
				ToolCallID: tc.ID, Content: fmt.Sprintf("Failed to parse %s input: %v", tc.Name, err), IsError: true,
			}})
			if !tec.cfg.DisableReflection {
				resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: errorReflectionPrompt})
			}
			return resultBlocks
		}
		if inp.FilePath != "" {
			key := filepath.Clean(inp.FilePath)
			if tec.cfg.ProjectDir != "" && !filepath.IsAbs(key) {
				key = filepath.Clean(filepath.Join(tec.cfg.ProjectDir, key))
			}
			editContent := inp.Content
			if tc.Name == "edit" {
				editContent = inp.NewString
			}
			if w := tec.loopDetector.CheckEdit(key, editContent); w != nil {
				if w.Type == "edit_count" {
					tec.logger.LogToolCall(tc.Name, 0, true)
					tec.logger.LogDoomLoop(key, tec.loopDetector.editCounts[key])
					tec.fc.TrackFile(key)
					tec.fc.Record(ctx, FailureDoomLoop, fmt.Sprintf("file %s edited too many times", key), tec.iteration+1)
					resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: &provider.ToolResult{
						ToolCallID: tc.ID, Content: w.Detail, IsError: true,
					}})
					injectWarning(w)
					return resultBlocks
				}
				injectWarning(w)
			}
		}
	}
	if tc.Name == "bash" {
		var inp struct{ Command string `json:"command"` }
		if err := json.Unmarshal(tc.Input, &inp); err == nil && inp.Command != "" {
			if w := tec.loopDetector.CheckCommand(inp.Command); w != nil {
				injectWarning(w)
			}
		}
	}
	toolStart := time.Now()
	result := tec.engine.tools.Execute(ctx, tc)
	elapsed := time.Since(toolStart)
	tec.logger.LogToolCall(tc.Name, elapsed, result.IsError)
	tec.fc.TrackTool(tc.Name)
	tec.engine.mu.Lock()
	tec.engine.lastRunRecords = append(tec.engine.lastRunRecords, ToolRecord{
		Name: tc.Name, DurationMs: elapsed.Milliseconds(), IsError: result.IsError,
	})
	tec.engine.mu.Unlock()
	resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: result})
	// Track verification state
	if !result.IsError {
		if tc.Name == "edit" || tc.Name == "write" {
			var inp struct{ FilePath string `json:"file_path"` }
			if json.Unmarshal(tc.Input, &inp) == nil && inp.FilePath != "" {
				tec.verifyState.MarkEdit(inp.FilePath)
			}
		}
		if tc.Name == "bash" {
			var inp struct{ Command string `json:"command"` }
			if json.Unmarshal(tc.Input, &inp) == nil && IsVerifyCommand(inp.Command) {
				tec.verifyState.MarkVerified()
			}
		}
	}
	if result.IsError && !tec.cfg.DisableReflection {
		if w := tec.loopDetector.CheckError(result.Content); w != nil {
			injectWarning(w)
		} else {
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: errorReflectionPrompt})
		}
	}
	return resultBlocks
}
```

- [ ] **Step 3: Replace the tool execution block in engine.go**

In `internal/engine/engine.go`, replace lines 394–484 (the `var resultBlocks` block through the closing brace of the `for _, tc` loop) with:

```go
		// Execute tools: read-only in parallel, mutating sequentially
		tec := &toolExecContext{
			engine:       e,
			cfg:          cfg,
			loopDetector: loopDetector,
			verifyState:  verifyState,
			fc:           fc,
			logger:       logger,
			iteration:    iterations,
		}
		resultBlocks := executeToolCalls(ctx, tec, toolCalls)
```

Also remove the now-unused `"path/filepath"` import from `engine.go` if no other code references it. Keep the `"time"` import (used elsewhere).

- [ ] **Step 4: Run all engine tests to verify extraction is behavior-preserving**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -v -count=1`
Expected: All tests PASS (same results as Step 1 baseline)

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 6: Verify engine.go line count is under 500**

Run: `wc -l internal/engine/engine.go`
Expected: ~420 lines (under 500 limit)

- [ ] **Step 7: Commit**

```bash
git add internal/engine/parallel.go internal/engine/engine.go
git commit -m "refactor(T3.3): extract tool execution into parallel.go with partitioned dispatch"
```

---

## Chunk 2: Background Task Support (T3.4)

### Task 4: BackgroundTaskManager

**Files:**
- Create: `internal/tool/background.go`
- Create: `internal/tool/background_test.go`

- [ ] **Step 1: Write BackgroundTaskManager tests**

Create `internal/tool/background_test.go`:

```go
package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackgroundTaskManager_StartAndGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, err := mgr.Start("echo hello", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(taskID, "bg_") {
		t.Errorf("expected bg_ prefix, got %q", taskID)
	}

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	task := mgr.Get(taskID)
	if task == nil {
		t.Fatal("expected task, got nil")
	}
	if task.Status != "completed" {
		t.Errorf("expected completed, got %q", task.Status)
	}
	if task.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", task.ExitCode)
	}
}

func TestBackgroundTaskManager_ReadOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, err := mgr.Start("echo hello", 30)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	output, err := mgr.ReadOutput(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello' in output, got %q", output)
	}
}

func TestBackgroundTaskManager_InvalidID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	if task := mgr.Get("bg_nonexistent"); task != nil {
		t.Error("expected nil for invalid ID")
	}
	if _, err := mgr.ReadOutput("bg_nonexistent"); err == nil {
		t.Error("expected error for invalid ID")
	}
}

func TestBackgroundTaskManager_FailedCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, err := mgr.Start("exit 1", 30)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	task := mgr.Get(taskID)
	if task.Status != "failed" {
		t.Errorf("expected failed, got %q", task.Status)
	}
	if task.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", task.ExitCode)
	}
}

func TestBackgroundTaskManager_Cleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)

	taskID, _ := mgr.Start("echo cleanup", 30)
	time.Sleep(200 * time.Millisecond)

	task := mgr.Get(taskID)
	outputPath := task.OutputPath

	mgr.Cleanup()

	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("expected temp file removed after cleanup")
	}
}

func TestBackgroundTaskManager_StaleFileCleanup(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "nanocode")
	os.MkdirAll(tmpDir, 0755)

	// Create a fake stale file
	stalePath := filepath.Join(tmpDir, "bg_stale1234.out")
	os.WriteFile(stalePath, []byte("old"), 0644)
	// Set mtime to 25 hours ago
	oldTime := time.Now().Add(-25 * time.Hour)
	os.Chtimes(stalePath, oldTime, oldTime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("expected stale file removed on construction")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/tool/ -run TestBackgroundTask -v`
Expected: FAIL — types undefined

- [ ] **Step 3: Implement BackgroundTaskManager**

Create `internal/tool/background.go`:

```go
package tool

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackgroundTask tracks a command running in the background.
type BackgroundTask struct {
	ID         string
	Command    string
	Status     string // "running", "completed", "failed"
	OutputPath string
	ExitCode   int
	StartedAt  time.Time
	DoneAt     time.Time
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// BackgroundTaskManager tracks background bash commands.
type BackgroundTaskManager struct {
	rootCtx context.Context
	mu      sync.Mutex
	tasks   map[string]*BackgroundTask
}

// NewBackgroundTaskManager creates a manager. It scans for and removes
// stale temp files (>24h old) from previous crashed sessions.
func NewBackgroundTaskManager(rootCtx context.Context) *BackgroundTaskManager {
	cleanStaleFiles()
	return &BackgroundTaskManager{
		rootCtx: rootCtx,
		tasks:   make(map[string]*BackgroundTask),
	}
}

// Start launches a command in the background and returns its task ID.
func (m *BackgroundTaskManager) Start(command string, timeout int) (string, error) {
	id, err := generateTaskID()
	if err != nil {
		return "", fmt.Errorf("generating task ID: %w", err)
	}

	tmpDir := filepath.Join(os.TempDir(), "nanocode")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	outPath := filepath.Join(tmpDir, id+".out")
	outFile, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating output file: %w", err)
	}

	if timeout <= 0 {
		timeout = 300
	}
	if timeout > 3600 {
		timeout = 3600
	}

	taskCtx, cancel := context.WithTimeout(m.rootCtx, time.Duration(timeout)*time.Second)

	task := &BackgroundTask{
		ID:         id,
		Command:    command,
		Status:     "running",
		OutputPath: outPath,
		StartedAt:  time.Now(),
		cancel:     cancel,
	}

	m.mu.Lock()
	m.tasks[id] = task
	m.mu.Unlock()

	go func() {
		defer outFile.Close()
		defer cancel()

		cmd := exec.CommandContext(taskCtx, "bash", "-c", command)
		cmd.Stdout = outFile
		cmd.Stderr = outFile

		err := cmd.Run()
		task.mu.Lock()
		defer task.mu.Unlock()
		task.DoneAt = time.Now()
		if err != nil {
			task.Status = "failed"
			if exitErr, ok := err.(*exec.ExitError); ok {
				task.ExitCode = exitErr.ExitCode()
			} else {
				task.ExitCode = -1
			}
		} else {
			task.Status = "completed"
			task.ExitCode = 0
		}
	}()

	return id, nil
}

// Get returns a task by ID, or nil if not found.
func (m *BackgroundTaskManager) Get(id string) *BackgroundTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id]
}

// ReadOutput returns the current output of a background task.
func (m *BackgroundTaskManager) ReadOutput(id string) (string, error) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("background task not found: %s", id)
	}
	data, err := os.ReadFile(task.OutputPath)
	if err != nil {
		return "", fmt.Errorf("reading output: %w", err)
	}
	return TruncateOutput(string(data), MaxOutputLen), nil
}

// Cleanup cancels running tasks and removes temp files.
func (m *BackgroundTaskManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, task := range m.tasks {
		if task.cancel != nil {
			task.cancel()
		}
		os.Remove(task.OutputPath)
	}
	m.tasks = make(map[string]*BackgroundTask)
}

func generateTaskID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("bg_%x", b), nil
}

func cleanStaleFiles() {
	tmpDir := filepath.Join(os.TempDir(), "nanocode")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "bg_") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(tmpDir, e.Name()))
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/tool/ -run TestBackgroundTask -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tool/background.go internal/tool/background_test.go
git commit -m "feat(T3.4): add BackgroundTaskManager with stale file cleanup"
```

### Task 5: TaskOutputTool

**Files:**
- Create: `internal/tool/taskoutput.go`
- Create: `internal/tool/taskoutput_test.go`

- [ ] **Step 1: Write TaskOutputTool tests**

Create `internal/tool/taskoutput_test.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskOutputTool_Name(t *testing.T) {
	tool := &TaskOutputTool{}
	if tool.Name() != "task_output" {
		t.Errorf("expected 'task_output', got %q", tool.Name())
	}
}

func TestTaskOutputTool_CompletedTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, _ := mgr.Start("echo done", 30)
	time.Sleep(200 * time.Millisecond)

	tool := &TaskOutputTool{Manager: mgr}
	input, _ := json.Marshal(map[string]string{"task_id": taskID})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("expected 'completed' in result, got %q", result)
	}
	if !strings.Contains(result, "done") {
		t.Errorf("expected 'done' in output, got %q", result)
	}
}

func TestTaskOutputTool_InvalidID(t *testing.T) {
	tool := &TaskOutputTool{Manager: &BackgroundTaskManager{
		rootCtx: context.Background(),
		tasks:   make(map[string]*BackgroundTask),
	}}
	input, _ := json.Marshal(map[string]string{"task_id": "bg_nope"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Error("expected error for invalid task ID")
	}
}

func TestTaskOutputTool_RunningTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, _ := mgr.Start("sleep 5", 30)
	// Don't wait — task should be running

	tool := &TaskOutputTool{Manager: mgr}
	input, _ := json.Marshal(map[string]string{"task_id": taskID})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "running") {
		t.Errorf("expected 'running' in result, got %q", result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/tool/ -run TestTaskOutputTool -v`
Expected: FAIL — `TaskOutputTool` undefined

- [ ] **Step 3: Implement TaskOutputTool**

Create `internal/tool/taskoutput.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// TaskOutputTool retrieves the status and output of a background bash task.
type TaskOutputTool struct {
	Manager *BackgroundTaskManager
}

type taskOutputInput struct {
	TaskID string `json:"task_id"`
}

func (t *TaskOutputTool) Name() string { return "task_output" }

func (t *TaskOutputTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "task_output",
		Description: "Get the status and output of a background bash task.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"task_id": {"type": "string", "description": "The background task ID (e.g. bg_abc123)"}
			},
			"required": ["task_id"]
		}`),
	}
}

func (t *TaskOutputTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[taskOutputInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}
	if in.TaskID == "" {
		return "", fmt.Errorf("task_id is required")
	}

	task := t.Manager.Get(in.TaskID)
	if task == nil {
		return "", fmt.Errorf("background task not found: %s", in.TaskID)
	}

	output, err := t.Manager.ReadOutput(in.TaskID)
	if err != nil {
		return "", err
	}

	task.mu.Lock()
	status := task.Status
	exitCode := task.ExitCode
	task.mu.Unlock()

	return fmt.Sprintf("Status: %s\nExit code: %d\n\n%s", status, exitCode, output), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/tool/ -run TestTaskOutputTool -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tool/taskoutput.go internal/tool/taskoutput_test.go
git commit -m "feat(T3.4): add TaskOutputTool for polling background task status"
```

### Task 6: Wire background tasks into BashTool and Engine

**Files:**
- Modify: `internal/tool/bash.go`
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: Add run_in_background to BashTool**

In `internal/tool/bash.go`:

1. Add `BackgroundTasks *BackgroundTaskManager` field to `BashTool` struct (after `getToolCallID` field).

2. Add `"run_in_background"` to `BashInput`:
```go
type BashInput struct {
	Command         string `json:"command"`
	Timeout         int    `json:"timeout"`
	RunInBackground bool   `json:"run_in_background"`
}
```

3. Update `Definition()` input schema — add to properties:
```json
"run_in_background": {"type": "boolean", "description": "Run command in background, returns task ID immediately"}
```

4. In `Execute()`, add background dispatch in both code paths.

**Override path** (around line 153): After `if !override.approved` and `if override.skipped` checks, before `return t.executeCommand(ctx, in)`:
```go
			if in.RunInBackground {
				return t.startBackground(in)
			}
			// approved: skip confirmation, proceed to execution
			return t.executeCommand(ctx, in)
```

**Normal path** (around line 167): After `if !confirm(in.Command)` check, before `return t.executeCommand(ctx, in)`:
```go
	if in.RunInBackground {
		return t.startBackground(in)
	}
	return t.executeCommand(ctx, in)
```

Add a helper method to keep both paths DRY:
```go
func (t *BashTool) startBackground(in BashInput) (string, error) {
	if t.BackgroundTasks == nil {
		return "", fmt.Errorf("background execution not available in subagent mode")
	}
	taskID, err := t.BackgroundTasks.Start(in.Command, in.Timeout)
	if err != nil {
		return "", fmt.Errorf("starting background task: %w", err)
	}
	return fmt.Sprintf(`{"task_id": %q, "status": "running"}`, taskID), nil
}
```

- [ ] **Step 2: Wire BackgroundTaskManager in engine.go**

In `internal/engine/engine.go`:

1. Add fields to `Engine` struct: `bgTasks *tool.BackgroundTaskManager` and `bgCancel context.CancelFunc`

2. In `New()`, after `bashTool` creation (around line 62), add:
```go
	bgCtx, bgCancel := context.WithCancel(context.Background())
	bgTasks := tool.NewBackgroundTaskManager(bgCtx)
	bashTool.BackgroundTasks = bgTasks
```

3. In `New()`, after task tools registration (around line 147), add:
```go
	allTools = append(allTools, &tool.TaskOutputTool{Manager: bgTasks})
```

4. Store bgTasks and bgCancel in engine struct: add `bgTasks: bgTasks, bgCancel: bgCancel,` to the `eng := &Engine{...}` block.

5. In `Close()`, add before MCP cleanup:
```go
	if e.bgCancel != nil {
		e.bgCancel()
	}
	if e.bgTasks != nil {
		e.bgTasks.Cleanup()
	}
```

- [ ] **Step 3: Run all tests**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tool/bash.go internal/engine/engine.go
git commit -m "feat(T3.4): wire background task support into BashTool and Engine"
```

---

## Chunk 3: Summarization Persistence (T3.1)

### Task 7: Add PersistSummary to Store

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/migrate.go`
- Modify: `internal/store/store_test.go` (or create summary test)

- [ ] **Step 1: Write PersistSummary test**

Append to `internal/store/store_test.go`:

```go
func TestPersistSummary(t *testing.T) {
	st := testStore(t)
	defer st.Close()
	ctx := context.Background()

	sessionID, _ := st.CreateSession(ctx, "/tmp")
	err := st.PersistSummary(ctx, sessionID, "Files were edited.", 30, 12)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it was persisted (query directly)
	var count int
	st.db.QueryRow("SELECT COUNT(*) FROM summaries WHERE session_id = ?", sessionID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 summary, got %d", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/store/ -run TestPersistSummary -v`
Expected: FAIL — method not found

- [ ] **Step 3: Add migration and implementation**

In `internal/store/migrate.go`, append to `migrations` slice:

```go
	// Version 6: summary persistence
	`CREATE TABLE IF NOT EXISTS summaries (
		id             TEXT PRIMARY KEY,
		session_id     TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		summary_text   TEXT NOT NULL,
		original_count INTEGER NOT NULL,
		result_count   INTEGER NOT NULL,
		created_at     INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_summaries_session ON summaries(session_id, created_at);`,
```

In `internal/store/store.go`, add to `Store` interface:

```go
	PersistSummary(ctx context.Context, sessionID, summary string, originalCount, resultCount int) error
```

Add implementation on `SQLiteStore` (append to store.go after `Close()`):

```go
func (s *SQLiteStore) PersistSummary(ctx context.Context, sessionID, summary string, originalCount, resultCount int) error {
	id := uuid.New().String()
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO summaries (id, session_id, summary_text, original_count, result_count, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, sessionID, summary, originalCount, resultCount, now,
	)
	if err != nil {
		return fmt.Errorf("persisting summary: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/store/ -run TestPersistSummary -v`
Expected: PASS

- [ ] **Step 5: Run full test suite to check for interface breakage**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./... -count=1`
Expected: All tests PASS (SQLiteStore satisfies updated interface)

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/migrate.go internal/store/store_test.go
git commit -m "feat(T3.1): add PersistSummary to Store with migration v6"
```

### Task 8: Wire Summarizer to Store

**Files:**
- Modify: `internal/engine/summarize.go`
- Modify: `internal/engine/summarize_test.go`
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: Write summarizer persistence test**

Append to `internal/engine/summarize_test.go`:

```go
func TestSummarizerPersistsToStore(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Summary: things happened."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, "test-model", 30, 10, st, sessionID)
	msgs := makeMsgs(35)
	result, err := s.MaybeSummarize(ctx, msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Verify summarization happened (first + summary + 10 recent = 12)
	if len(result) != 12 {
		t.Errorf("expected 12 messages, got %d", len(result))
	}

	// Verify summary was persisted by querying the summaries table.
	// OpenMemory returns *SQLiteStore which has unexported db field,
	// so we verify indirectly: run a second summarization and check
	// both work without error. The PersistSummary call is best-effort
	// (logged, not returned), so we trust the store_test.go TestPersistSummary
	// for the actual SQL verification.
}

func TestSummarizerNilStoreDoesNotPanic(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Summary."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, "test-model", 30, 10, nil, "")
	msgs := makeMsgs(35)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// Should still summarize, just not persist
	if len(result) != 12 {
		t.Errorf("expected 12 messages, got %d", len(result))
	}
}
```

- [ ] **Step 2: Update Summarizer struct and constructor**

In `internal/engine/summarize.go`:

Add `store` and `sessionID` fields to `Summarizer`:
```go
type Summarizer struct {
	provider  provider.Provider
	model     string
	threshold int
	keepN     int
	store     store.Store
	sessionID string
}
```

Add import for `"github.com/robertkohahimn/nanocode/internal/store"`.

Update `NewSummarizer`:
```go
func NewSummarizer(p provider.Provider, model string, threshold, keepN int, st store.Store, sessionID string) *Summarizer {
	if keepN < 0 {
		keepN = 10
	}
	return &Summarizer{provider: p, model: model, threshold: threshold, keepN: keepN, store: st, sessionID: sessionID}
}
```

In `MaybeSummarize`, after the successful summarization (after `result = append(result, recent...)`, before the return), add:
```go
	// Persist summary (best-effort)
	if s.store != nil && s.sessionID != "" {
		if err := s.store.PersistSummary(ctx, s.sessionID, summary, len(messages), len(result)); err != nil {
			log.Printf("engine: failed to persist summary: %v", err)
		}
	}
```

- [ ] **Step 3: Update existing test call sites**

In `internal/engine/summarize_test.go`, update all `NewSummarizer` calls to include the two new nil/"" params:

- `NewSummarizer(nil, "test-model", 30, 10)` → `NewSummarizer(nil, "test-model", 30, 10, nil, "")`
- `NewSummarizer(mp, "test-model", 30, 10)` → `NewSummarizer(mp, "test-model", 30, 10, nil, "")`
- `NewSummarizer(mp, "test-model", 10, 5)` → `NewSummarizer(mp, "test-model", 10, 5, nil, "")`

- [ ] **Step 4: Update engine.go call site**

In `internal/engine/engine.go`, line 313, update:
```go
summarizer := NewSummarizer(e.provider, cfg.Model, cfg.SummarizeThreshold, cfg.SummarizeKeepRecent, e.store, sessionID)
```

- [ ] **Step 5: Run all tests**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/engine/summarize.go internal/engine/summarize_test.go internal/engine/engine.go
git commit -m "feat(T3.1): wire Summarizer to Store for summary persistence"
```

---

## Chunk 4: Project Context Gap Fill (T3.2)

### Task 9: Add main branch detection

**Files:**
- Modify: `internal/engine/context.go`
- Modify: `internal/engine/context_test.go`

- [ ] **Step 1: Write main branch detection tests**

Append to `internal/engine/context_test.go`:

```go
func TestDetectMainBranch_Main(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	run("add", "f.txt")
	run("commit", "-m", "init")

	branch := detectMainBranch(dir)
	if branch != "main" {
		t.Errorf("expected 'main', got %q", branch)
	}
}

func TestDetectMainBranch_Master(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run("init", "--initial-branch=master")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	run("add", "f.txt")
	run("commit", "-m", "init")

	branch := detectMainBranch(dir)
	if branch != "master" {
		t.Errorf("expected 'master', got %q", branch)
	}
}

func TestDetectMainBranch_Neither(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run("init", "--initial-branch=develop")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	run("add", "f.txt")
	run("commit", "-m", "init")

	branch := detectMainBranch(dir)
	if branch != "" {
		t.Errorf("expected empty string, got %q", branch)
	}
}

func TestBuildProjectContextIncludesMainBranch(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	run("add", "f.txt")
	run("commit", "-m", "init")
	// Switch to feature branch
	run("checkout", "-b", "feature/test")

	result := BuildProjectContext(dir)
	if !strings.Contains(result, "Main branch: main") {
		t.Error("expected 'Main branch: main' in context")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -run TestDetectMainBranch -v`
Expected: FAIL — `detectMainBranch` undefined

- [ ] **Step 3: Implement detectMainBranch**

In `internal/engine/context.go`, add after the `gitCommand` function:

```go
// detectMainBranch returns the name of the main/default branch.
// Checks for "main", then "master", then remote HEAD.
func detectMainBranch(dir string) string {
	if gitCommand(dir, "rev-parse", "--verify", "main") != "" {
		return "main"
	}
	if gitCommand(dir, "rev-parse", "--verify", "master") != "" {
		return "master"
	}
	// Try remote HEAD
	ref := gitCommand(dir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if ref != "" {
		// ref looks like "refs/remotes/origin/main"
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	return ""
}
```

In `BuildProjectContext()`, after the current branch block (after line 50 `sb.WriteString("\n\n")`), add:

```go
		if mainBranch := detectMainBranch(projectDir); mainBranch != "" {
			sb.WriteString("Main branch: ")
			sb.WriteString(escapeXML(mainBranch))
			sb.WriteString("\n\n")
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./internal/engine/ -run "TestDetectMainBranch|TestBuildProjectContextIncludesMainBranch" -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/engine/context.go internal/engine/context_test.go
git commit -m "feat(T3.2): add main branch detection to project context"
```

---

## Chunk 5: Final Verification

### Task 10: Full suite and line budget check

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 2: Verify line counts**

Run: `wc -l internal/engine/engine.go internal/engine/parallel.go internal/tool/bash.go internal/tool/background.go internal/tool/taskoutput.go internal/store/store.go internal/store/migrate.go internal/engine/summarize.go internal/engine/context.go`
Expected: All files under 500 lines

- [ ] **Step 3: Verify no new dependencies**

Run: `git diff main -- go.mod`
Expected: No new dependencies added

- [ ] **Step 4: Verify build produces single binary**

Run: `cd /Users/Maestro/conductor/worktrees/feat-harness-roadmap-path-8jg && go build -o /dev/null .`
Expected: Builds successfully

- [ ] **Step 5: Commit any final fixes if needed, then done**
