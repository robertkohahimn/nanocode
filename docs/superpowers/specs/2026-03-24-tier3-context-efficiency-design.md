# Tier 3: Context & Efficiency — Design Spec

## Overview

Implement the five Tier 3 harness roadmap items (issues #18–#22) to handle larger tasks and reduce token waste. Three features are gap-fills on existing code; two are new.

## Implementation Order

T3.3 (parallel execution) must come first — it extracts ~80 lines from `engine.go` into `parallel.go`, creating headroom for T3.4 wiring. T3.1 and T3.2 are independent gap-fills. T3.5 is already complete.

Order: **T3.3 → T3.4 → T3.1 → T3.2** (T3.5 is a no-op).

## T3.1: Summarization Gap Fill (#18)

### Current State
`Summarizer` in `internal/engine/summarize.go` handles LLM-based message compression with threshold/keepN config, tool_result boundary awareness, and windowing fallback.

### Gaps
- Summaries not persisted to store (lost on session resume)
- No message count tracking in store

### Changes

**`internal/store/store.go`** — Add `PersistSummary` to the `Store` interface:
```go
PersistSummary(ctx context.Context, sessionID, summary string, originalCount, resultCount int) error
```

**`internal/store/store.go`** — Add `PersistSummary` implementation on `SQLiteStore`.

**`internal/store/migrate.go`** — Add Version 6 migration for `summaries` table:
```sql
CREATE TABLE IF NOT EXISTS summaries (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    summary_text   TEXT NOT NULL,
    original_count INTEGER NOT NULL,
    result_count   INTEGER NOT NULL,
    created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_summaries_session ON summaries(session_id, created_at);
```

**`internal/engine/summarize.go`** — Add optional `store` and `sessionID` fields:
- After successful summarization, call `store.PersistSummary()` with counts
- Nil-safe: works without store (subagent runs)
- Constructor: `NewSummarizer(p, model, threshold, keepN, store, sessionID)`

**`internal/engine/engine.go`** — Update `NewSummarizer` call site (line 313) to pass `e.store` and `sessionID`.

### Tests
- Verify summary persisted after successful summarization (use `OpenMemory()` store)
- Verify nil store doesn't panic
- Verify counts are correct
- Update any mock Store implementations to include `PersistSummary`

---

## T3.2: Project Context Gap Fill (#19)

### Current State
`BuildProjectContext()` in `internal/engine/context.go` injects git status, branch, recent commits, environment, and nanocode.md content.

### Gap
- No main branch detection for PR context

### Changes

**`internal/engine/context.go`** — Add `detectMainBranch(dir string) string` using the existing `gitCommand()` helper:
1. `gitCommand(dir, "rev-parse", "--verify", "main")` — if non-empty, return `"main"`
2. `gitCommand(dir, "rev-parse", "--verify", "master")` — if non-empty, return `"master"`
3. `gitCommand(dir, "symbolic-ref", "refs/remotes/origin/HEAD")` — parse branch name
4. Return `""` if none found

Include in `BuildProjectContext()` output after the current branch line:
```
Main branch: main
```

### Tests
- Temp git repo with main branch detected
- Fallback when neither main/master exists
- Uses existing `gitCommand()` helper (no raw exec.Command)

---

## T3.3: Parallel Tool Execution (#20)

### Design

**New file: `internal/engine/parallel.go`** (~100 lines)

Read-only tool set: `map[string]bool{"read": true, "glob": true, "grep": true}`. Hardcoded for now; MCP tools are always sequential (future consideration).

**Partitioning:** Scan tool calls in order. Consecutive read-only calls form a parallel batch. Mutating calls are sequential items.

Example:
```
toolCalls: [read, glob, grep, edit, read, read, bash]
groups:    [parallel(read,glob,grep), sequential(edit), parallel(read,read), sequential(bash)]
```

**Execution:**
- Parallel batches: `sync.WaitGroup` + goroutines, capped at 10 via semaphore channel. Results indexed by original position for deterministic ordering. After execution, parallel results still feed through `fc.TrackTool()`, `ToolRecord` collection, and error reflection — these side effects are applied after the batch completes (not inside goroutines) to avoid concurrent map writes.
- Sequential items: existing path with doom loop detection, verification tracking, error reflection.

**Engine integration:** Extract the tool execution block (lines 401–484 of `engine.go`, ~83 lines) into a new `executeToolCalls()` function in `parallel.go`. This extraction reduces `engine.go` from 496 to ~420 lines, creating headroom for T3.4 wiring. The loop in `engine.go` calls `executeToolCalls()` which handles partitioning, parallel batches, and sequential dispatch internally.

### Tests
- Partitioning logic (various tool call orderings)
- Concurrent execution (mock tools with sleep to prove parallelism)
- Result ordering preserved
- Semaphore cap respected
- Side effects (ToolRecord, TrackTool) applied correctly for parallel batches

---

## T3.4: Background Task Support (#21)

### Design

**New file: `internal/tool/background.go`** (~120 lines)

```go
type BackgroundTask struct {
    ID         string
    Command    string
    Status     string    // "running", "completed", "failed"
    OutputFile *os.File
    ExitCode   int
    StartedAt  time.Time
    DoneAt     time.Time
    mu         sync.Mutex
}

type BackgroundTaskManager struct {
    mu    sync.Mutex
    tasks map[string]*BackgroundTask
}
```

Methods: `Start(ctx, command, timeout) (taskID, error)`, `Get(taskID)`, `ReadOutput(taskID)`, `Cleanup()`.

IDs: `bg_` + 8 hex chars from `crypto/rand`. Output streams to temp file under `os.TempDir()/nanocode/`.

**Lifecycle:** `Engine.Close()` calls `BackgroundTaskManager.Cleanup()` which:
1. Cancels any still-running tasks (via stored cancel funcs from `context.WithCancel`)
2. Waits briefly for goroutines to exit (100ms timeout)
3. Removes temp files for completed/cancelled tasks

**Subagent isolation:** Background execution is disabled in subagent mode. `BashTool` checks: if `run_in_background` is true but `BackgroundTasks` is nil (subagent case), return an error: "Background execution not available in subagent mode." The `BackgroundTaskManager` is only wired in the top-level engine `New()`, not in subagent tool sets.

**BashTool changes (`internal/tool/bash.go`):**
- Add `run_in_background` bool to `BashInput` and input schema
- When true and `BackgroundTasks` is non-nil, delegate to `BackgroundTaskManager.Start()`
- Return immediately: `{"task_id": "bg_abc123", "status": "running"}`
- Confirmation still required before spawning
- New field: `BackgroundTasks *BackgroundTaskManager`

**New file: `internal/tool/taskoutput.go`** (~80 lines)

`TaskOutputTool`:
- Name: `task_output`
- Input: `{"task_id": "bg_abc123"}`
- Returns: status + output (truncated to MaxOutputLen)
- If still running, returns partial output so far
- References same `BackgroundTaskManager` as BashTool

**Engine wiring (`internal/engine/engine.go`):**
Create `BackgroundTaskManager` in `New()`, pass to both `BashTool` and `TaskOutputTool`, register `TaskOutputTool` in tool list. Add `bgTasks` field to `Engine` struct. Call `bgTasks.Cleanup()` in `Engine.Close()`.

### Tests
- Start background command, poll with TaskOutputTool, verify completion
- Test cleanup of temp files and cancellation of running tasks
- Test confirmation still applies
- Test invalid task ID handling
- Test subagent mode returns error for run_in_background

---

## T3.5: Checkpoint Verification (#22)

### Current State
Fully implemented in `internal/engine/checkpoint.go` with escalating urgency (gentle/warning/urgent), configurable interval, and comprehensive tests in `checkpoint_test.go`.

### Assessment
All acceptance criteria met. No changes needed.

---

## File Inventory

| File | Action | Est. Lines |
|------|--------|-----------|
| `internal/engine/parallel.go` | New | ~180 (includes extracted loop logic) |
| `internal/engine/parallel_test.go` | New | ~120 |
| `internal/engine/engine.go` | Edit | -80 (extract) +10 (wiring) = net -70 |
| `internal/tool/background.go` | New | ~120 |
| `internal/tool/background_test.go` | New | ~100 |
| `internal/tool/taskoutput.go` | New | ~80 |
| `internal/tool/taskoutput_test.go` | New | ~70 |
| `internal/tool/bash.go` | Edit | +20 |
| `internal/store/store.go` | Edit | +15 (interface + impl) |
| `internal/store/migrate.go` | Edit | +10 (Version 6) |
| `internal/engine/summarize.go` | Edit | +15 |
| `internal/engine/summarize_test.go` | Edit | +30 |
| `internal/engine/context.go` | Edit | +25 |
| `internal/engine/context_test.go` | Edit | +30 |

### Line Budget Verification
- `engine.go`: 496 - 80 + 10 = ~426 (safe)
- `parallel.go`: ~180 (new, safe)
- `bash.go`: 219 + 20 = ~239 (safe)
- `store.go`: 285 + 15 = ~300 (safe)
- `migrate.go`: 128 + 10 = ~138 (safe)
- `summarize.go`: 160 + 15 = ~175 (safe)
- `context.go`: 122 + 25 = ~147 (safe)

## Constraints
- No file exceeds 500 lines
- No new dependencies (all stdlib: sync, crypto/rand, os)
- All new code has tests
- `go test ./...` passes after each logical step
