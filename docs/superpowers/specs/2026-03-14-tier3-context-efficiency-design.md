# Tier 3: Context & Efficiency — Design Spec

**Date:** 2026-03-14
**Issues:** #18 (T3.1), #19 (T3.2), #22 (T3.5)
**Scope:** First wave — checkpoint reminders, project context injection, context summarization
**Deferred:** #20 (T3.3 parallel execution), #21 (T3.4 background tasks) — second wave

## T3.5: Checkpoint Verification Reminders

### Problem
The model can spend 30+ iterations making no real progress with no check-in mechanism.

### Design

**New file:** `internal/engine/checkpoint.go` (~50 lines)

`CheckpointInjector` struct with configurable interval (default 10) and three escalation levels:

| Iteration | Level   | Behavior |
|-----------|---------|----------|
| 10        | Gentle  | "What have you accomplished? What's remaining?" |
| 25        | Warning | "You're halfway through. Are you stuck?" |
| 40        | Urgent  | "Consider stopping and reporting partial progress" |

**Integration:** Called in `loop()` after `logger.LogIteration()`, before the LLM call. If a checkpoint fires, its text is appended as a user-role message with `<system-reminder>` tags. Checkpoint messages are NOT persisted (matches the verify reminder pattern — in-memory only).

**Iteration scope:** Iteration counts are per `loop()` invocation, not cumulative across `Resume` calls. This is intentional — each turn gets its own budget. The hardcoded thresholds (10/25/40) are relative to `maxIterations` (50), leaving 10 iterations between urgent warning and hard stop.

**Config:** `CheckpointInterval int` on `config.Config` (0 = disabled).

**Logging:** `LogCheckpoint(iteration, level)` method on `EngineLogger`.

**Tests:** `checkpoint_test.go` — verify injection at correct iterations, escalation levels, interval=0 disables.

## T3.2: Enhanced Project Context Injection

### Problem
The model lacks awareness of project state. Currently only reads `nanocode.md`.

### Design

**New file:** `internal/engine/context.go` (~80 lines)

`BuildProjectContext(projectDir string) string` function that:

1. Guards with `git rev-parse --is-inside-work-tree` (skip all git if not a repo)
2. Collects:
   - `git branch --show-current`
   - `git status --short` (truncated to 20 lines)
   - `git log --oneline -5`
3. Appends environment: working directory, `runtime.GOOS`, `time.Now().Format("2006-01-02")`
4. Each git call gets a 2-second context timeout

**Integration:** Replaces the existing `nanocode.md` injection block in `loop()` (lines 268-282 of engine.go). `BuildProjectContext` returns the full system prompt suffix: git info + environment + nanocode.md content. The existing nanocode.md read logic (1MB limit, truncation marker) is preserved exactly, just moved into this function. This removes ~15 lines from engine.go, providing headroom.

**No caching struct needed** — runs once per `loop()` call. Subagent calls via `RunSubagent` also call `loop()` and will re-run git commands; this is acceptable since subagent spawns are infrequent.

**Error handling:** Each git command fails independently and gracefully (returns empty string). A repo with no commits will have `git log` return empty, but `git branch` and `git status` still work. The guard (`git rev-parse`) only gates whether git commands are attempted at all.

**Tests:** `context_test.go` — temp git repo (init, commit, verify output), non-git directory (graceful skip), long status truncation, repo with no commits.

## T3.1: Automatic Context Summarization

### Problem
The hard 40-message window drops important context from early in the conversation, killing complex tasks.

### Design

**New file:** `internal/engine/summarize.go` (~120 lines)

`Summarizer` struct holds a provider reference.

**`MaybeSummarize(ctx, messages) ([]provider.Message, error)`:**

1. If `len(messages) < summarizeThreshold` (default 30), return as-is
2. Split: `first` (message[0]), `middle` (to compress), `recent` (last 10)
3. Call LLM with summary prompt against middle section
4. Return `[first, summaryMessage, recent...]`

**Summary prompt:**
```
Summarize this conversation segment concisely:
- Key decisions made
- Files read/modified
- Errors encountered and how resolved
- Current state of the task

Keep technical details. Omit pleasantries.
```

**Summary message format:** User-role message wrapped in `<context-summary>` tags so the model knows it's synthetic.

**Fallback:** If the provider call fails, degrade to `windowMessages` — never crash the loop. The old `windowMessages` function is retained as the fallback path.

**Post-summarization safety net:** After `MaybeSummarize` returns, `windowMessages` is still applied as a final guard in case the summarized result + recent messages exceed the context window. Integration point: `MaybeSummarize` replaces the current `windowMessages` call site (line 306 of engine.go), but `windowMessages` is called on the result as a safety net.

**Config:**
- `SummarizeThreshold int` (default 30, 0 = disabled, use old windowing)
- `SummarizeKeepRecent int` (default 10)

**LLM call:** Uses the same provider instance with `MaxTokens: 1024` to keep summaries concise. The middle section is truncated to 100KB before being sent to the summarizer to bound cost.

**Re-summarization:** When messages hit the threshold again after a prior summarization, the middle section will contain the previous `<context-summary>` message. This is acceptable — the prior summary is already compressed and will be incorporated into the new summary naturally. Quality degradation over many re-summarizations is a known trade-off; in practice, sessions rarely trigger more than 2-3 summarizations before completing.

**Persistence:** Summaries are persisted via the existing `persistMessage` path.

**Logging:** `LogSummarization(originalCount, newCount int)` on `EngineLogger`.

**Edge cases:**
- Provider failure → fall back to `windowMessages`
- Empty middle section → skip summarization
- Tool result messages in middle are included in summary context; summary itself is plain text

**Config merge:** The `merge()` function in config.go needs updates for the three new int fields. Zero-value semantics: since 0 means "disabled/use defaults," a project config cannot explicitly disable summarization if the global config enables it. This is acceptable for v1.

**Tests:** `summarize_test.go` — threshold gating, message splitting, fallback on provider error, summary message structure, re-summarization with existing summary messages. Mock provider returns canned summary.

## File Impact Summary

| File | Change |
|------|--------|
| `internal/engine/checkpoint.go` | **New** — CheckpointInjector |
| `internal/engine/checkpoint_test.go` | **New** — tests |
| `internal/engine/context.go` | **New** — BuildProjectContext |
| `internal/engine/context_test.go` | **New** — tests |
| `internal/engine/summarize.go` | **New** — Summarizer, MaybeSummarize |
| `internal/engine/summarize_test.go` | **New** — tests |
| `internal/engine/engine.go` | **Modified** — wire checkpoint, context, summarize into loop |
| `internal/engine/logger.go` | **Modified** — add LogCheckpoint, LogSummarization |
| `internal/config/config.go` | **Modified** — add CheckpointInterval, SummarizeThreshold, SummarizeKeepRecent |

## Implementation Order

1. **T3.5** (checkpoint) — smallest, no dependencies
2. **T3.2** (context injection) — moderate, isolated to session start
3. **T3.1** (summarization) — largest, replaces windowing logic

## Constraints

- No file > 500 lines
- All new files get `_test.go` companions
- `go test ./...` must pass after each feature
- Single binary — git commands use `os/exec`, no new dependencies
