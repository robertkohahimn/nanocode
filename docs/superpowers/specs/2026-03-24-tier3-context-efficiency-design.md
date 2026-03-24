# Tier 3: Context & Efficiency — Design Spec

## Overview

Implement the five Tier 3 harness roadmap items (issues #18–#22) to handle larger tasks and reduce token waste. Three features are gap-fills on existing code; two are new.

## T3.1: Summarization Gap Fill (#18)

### Current State
`Summarizer` in `internal/engine/summarize.go` handles LLM-based message compression with threshold/keepN config, tool_result boundary awareness, and windowing fallback.

### Gaps
- Summaries not persisted to store (lost on session resume)
- No message count tracking in store

### Changes

**`internal/store/store.go`** — Add `PersistSummary` method:
```go
PersistSummary(ctx context.Context, sessionID, summary string, originalCount, resultCount int) error
```
Writes to a new `summaries` table: session_id, summary_text, original_count, result_count, created_at.

**`internal/engine/summarize.go`** — Add optional `store` and `sessionID` fields:
- After successful summarization, call `store.PersistSummary()` with counts
- Nil-safe: works without store (subagent runs)
- Constructor: `NewSummarizer(p, model, threshold, keepN, store, sessionID)`

### Tests
- Verify summary persisted after successful summarization
- Verify nil store doesn't panic
- Verify counts are correct

---

## T3.2: Project Context Gap Fill (#19)

### Current State
`BuildProjectContext()` in `internal/engine/context.go` injects git status, branch, recent commits, environment, and nanocode.md content.

### Gap
- No main branch detection for PR context

### Changes

**`internal/engine/context.go`** — Add `detectMainBranch(dir string) string`:
1. `git rev-parse --verify main` — if exists, return `"main"`
2. `git rev-parse --verify master` — if exists, return `"master"`
3. `git symbolic-ref refs/remotes/origin/HEAD` — parse branch name
4. Return `""` if none found

Include after current branch line: `Main branch: main`

### Tests
- Temp git repo with main branch
- Fallback when neither main/master exists

---

## T3.3: Parallel Tool Execution (#20)

### Design

**New file: `internal/engine/parallel.go`** (~100 lines)

Read-only tool set: `map[string]bool{"read": true, "glob": true, "grep": true}`

**Partitioning:** Scan tool calls in order. Consecutive read-only calls form a parallel batch. Mutating calls are sequential items.

Example:
```
toolCalls: [read, glob, grep, edit, read, read, bash]
groups:    [parallel(read,glob,grep), sequential(edit), parallel(read,read), sequential(bash)]
```

**Execution:**
- Parallel batches: `sync.WaitGroup` + goroutines, capped at 10 via semaphore channel. Results indexed by original position for deterministic ordering.
- Sequential items: existing path with doom loop detection, verification tracking, error reflection.

**Engine integration:** Refactor `for _, tc := range toolCalls` block in `engine.go` loop into `executeToolCallGroups()` that returns `[]provider.ContentBlock`. All existing per-tool logic stays on the sequential path.

### Tests
- Partitioning logic (various tool call orderings)
- Concurrent execution (mock tools with sleep to prove parallelism)
- Result ordering preserved
- Semaphore cap respected

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

**BashTool changes (`internal/tool/bash.go`):**
- Add `run_in_background` bool to `BashInput` and input schema
- When true, delegate to `BackgroundTaskManager.Start()`
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
Create `BackgroundTaskManager` in `New()`, pass to both `BashTool` and `TaskOutputTool`, register in tool list.

### Tests
- Start background command, poll with TaskOutputTool, verify completion
- Test cleanup of temp files
- Test confirmation still applies
- Test invalid task ID handling

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
| `internal/engine/parallel.go` | New | ~100 |
| `internal/engine/parallel_test.go` | New | ~120 |
| `internal/engine/engine.go` | Edit | +20 |
| `internal/tool/background.go` | New | ~120 |
| `internal/tool/background_test.go` | New | ~100 |
| `internal/tool/taskoutput.go` | New | ~80 |
| `internal/tool/taskoutput_test.go` | New | ~70 |
| `internal/tool/bash.go` | Edit | +15 |
| `internal/store/store.go` | Edit | +20 |
| `internal/engine/summarize.go` | Edit | +15 |
| `internal/engine/summarize_test.go` | Edit | +30 |
| `internal/engine/context.go` | Edit | +25 |
| `internal/engine/context_test.go` | Edit | +30 |

## Constraints
- No file exceeds 500 lines
- No new dependencies (all stdlib: sync, crypto/rand, os)
- All new code has tests
- `go test ./...` passes after each logical step
