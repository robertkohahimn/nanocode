# Tier 1 Foundation Bugfixes

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all 11 issues found during manual testing of the Tier 1 Foundation features.

**Architecture:** Targeted fixes across 4 packages (engine, benchmark, tool, CLI). No new packages. Each task is independent.

**Tech Stack:** Go, JSON structured logging, shell scripts

---

## Chunk 1: Logger and Engine Fixes

### Task 1: Split logEntry into per-type structs to eliminate irrelevant zero-valued fields (Finding 9)

**Files:**
- Modify: `internal/engine/logger.go`
- Modify: `internal/engine/logger_test.go`

The current flat `logEntry` struct includes every field for every log type, resulting in cluttered JSON output like `"edit_count":0,"total_messages":0,"windowed_messages":0` on every entry.

- [ ] **Step 1: Replace flat logEntry with per-type structs in logger.go**

Replace the single `logEntry` struct with type-specific structs. Each struct contains only the fields relevant to that log type. The `emit` method becomes a generic JSON emitter.

```go
// baseEntry contains fields common to all log entries.
type baseEntry struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Timestamp int64  `json:"timestamp"`
}

type iterationEntry struct {
	baseEntry
	Iteration int `json:"iteration"`
	ToolCalls int `json:"tool_calls"`
}

type toolCallEntry struct {
	baseEntry
	ToolName   string `json:"tool"`
	DurationMs int64  `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
}

type doomLoopEntry struct {
	baseEntry
	File      string `json:"file"`
	EditCount int    `json:"edit_count"`
}

type contextWindowEntry struct {
	baseEntry
	TotalMessages    int `json:"total_messages"`
	WindowedMessages int `json:"windowed_messages"`
}

type sessionEndEntry struct {
	baseEntry
	Iterations      int   `json:"iterations"`
	TotalDurationMs int64 `json:"total_duration_ms"`
}
```

Update `emit` to accept `any` instead of `logEntry`. Each Log method constructs its specific struct and calls `emit`.

- [ ] **Step 2: Update logger_test.go to unmarshal into per-type structs**

Update each test to unmarshal into the correct struct type. Verify that irrelevant fields are absent from the JSON output.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/engine/ -run "TestLog" -v`
Expected: All 9 logger tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/engine/logger.go internal/engine/logger_test.go
git commit -m "fix: split logEntry into per-type structs to eliminate irrelevant fields"
```

---

### Task 2: Fix duration_ms omission on sub-millisecond tool calls (Finding 11)

**Files:**
- Modify: `internal/engine/logger.go` (already modified in Task 1)

This is handled by Task 1 — the new `toolCallEntry` struct has `DurationMs int64 \`json:"duration_ms"\`` without `omitempty`, so 0 is always included.

- [ ] **Step 1: Verify duration_ms is present for 0-duration tool calls**

Add a test in logger_test.go:

```go
func TestLogToolCallZeroDuration(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-zero", &buf)
	l.LogToolCall("read", 0, false)

	raw := buf.String()
	if !strings.Contains(raw, `"duration_ms":0`) {
		t.Errorf("expected duration_ms:0 in output, got: %s", raw)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/engine/ -run "TestLogToolCall" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/logger_test.go
git commit -m "test: verify duration_ms always present on tool_call entries"
```

---

### Task 3: Fix session_end iterations off-by-one (Finding 10)

**Files:**
- Modify: `internal/engine/engine.go:304-306` (the defer that calls LogSessionEnd)

The `for iterations = 0` loop variable isn't post-incremented before `return nil`, so the deferred `LogSessionEnd(iterations, ...)` reports one fewer iteration than actually ran.

- [ ] **Step 1: Write failing test**

Add to a test file (e.g., `internal/engine/logger_test.go`):

```go
func TestLogSessionEndIterationCount(t *testing.T) {
	var buf bytes.Buffer
	l := NewEngineLogger("sess-count", &buf)
	// Simulate 3 iterations (0, 1, 2) where the loop exits at iteration 2
	// The correct count should be 3, not 2
	l.LogSessionEnd(3, time.Second)

	var entry sessionEndEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.Iterations != 3 {
		t.Errorf("iterations = %d, want 3", entry.Iterations)
	}
}
```

Note: The actual off-by-one is in `engine.go`, not the logger. The logger just records what it's told. Fix is in the defer.

- [ ] **Step 2: Fix the defer in engine.go loop()**

Change the deferred call from:
```go
defer func() {
    logger.LogSessionEnd(iterations, time.Since(loopStart))
}()
```

To:
```go
defer func() {
    logger.LogSessionEnd(iterations+1, time.Since(loopStart))
}()
```

Wait — this is wrong. The `iterations` variable is the loop counter. When the loop body returns (no tool calls), `iterations` is at the value before post-increment. The correct fix: increment before the deferred log call won't work because `iterations` is assigned by the for loop. Instead, track a separate counter:

Actually the simplest fix: the for loop starts at `iterations = 0` and when return happens on the first pass, `iterations = 0`. But we ran 1 iteration. So the fix is `iterations + 1` in the defer.

```go
defer func() {
    logger.LogSessionEnd(iterations+1, time.Since(loopStart))
}()
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/engine/engine.go
git commit -m "fix: correct off-by-one in session_end iterations count"
```

---

### Task 4: Add tool call recording to engine for benchmark granularity (Finding 2)

**Files:**
- Modify: `internal/engine/engine.go` — add ToolRecord collection to loop()
- Modify: `main.go:289-303` — update benchmarkEngineAdapter to use recorded calls

The engine currently discards individual tool call info. Add a mechanism to collect records.

- [ ] **Step 1: Add ToolRecord type and collection to engine.go**

Add a ToolRecord type to the engine package:

```go
// ToolRecord captures a single tool invocation during an engine run.
type ToolRecord struct {
	Name       string
	DurationMs int64
	IsError    bool
}
```

Add a `toolRecords []ToolRecord` field to the `loop` closure (or pass-through via callback). The simplest approach: add a `lastRunRecords` field to the Engine struct that `loop()` populates and the adapter reads after `Run()` returns.

```go
// In Engine struct:
lastRunRecords []ToolRecord
mu             sync.Mutex
```

In `loop()`, after each tool execution:
```go
toolStart := time.Now()
result := e.tools.Execute(ctx, tc)
elapsed := time.Since(toolStart)
logger.LogToolCall(tc.Name, elapsed, result.IsError)
e.mu.Lock()
e.lastRunRecords = append(e.lastRunRecords, ToolRecord{
    Name:       tc.Name,
    DurationMs: elapsed.Milliseconds(),
    IsError:    result.IsError,
})
e.mu.Unlock()
```

Add method to retrieve:
```go
// ConsumeToolRecords returns and clears the tool records from the last run.
func (e *Engine) ConsumeToolRecords() []ToolRecord {
    e.mu.Lock()
    defer e.mu.Unlock()
    records := e.lastRunRecords
    e.lastRunRecords = nil
    return records
}
```

Clear records at the start of `loop()`:
```go
e.mu.Lock()
e.lastRunRecords = nil
e.mu.Unlock()
```

- [ ] **Step 2: Update benchmarkEngineAdapter in main.go**

Replace the aggregate record with actual tool records:

```go
func (a *benchmarkEngineAdapter) Run(ctx context.Context, prompt string) ([]benchmark.ToolCallRecord, error) {
    sessionID, err := a.store.CreateSession(ctx, a.projectDir)
    if err != nil {
        return nil, fmt.Errorf("creating session: %w", err)
    }
    if err := a.eng.Run(ctx, sessionID, prompt, func(_ provider.Event) {}); err != nil {
        return nil, err
    }
    engineRecords := a.eng.ConsumeToolRecords()
    records := make([]benchmark.ToolCallRecord, len(engineRecords))
    for i, r := range engineRecords {
        records[i] = benchmark.ToolCallRecord{
            Name:       r.Name,
            DurationMs: r.DurationMs,
            IsError:    r.IsError,
        }
    }
    return records, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/engine/ ./internal/tool/ ./internal/benchmark/ -v`
Expected: PASS. Also `go build -o nanocode .` must succeed.

- [ ] **Step 4: Commit**

```bash
git add internal/engine/engine.go main.go
git commit -m "feat: expose per-tool call records from engine for benchmark granularity"
```

---

## Chunk 2: CLI and Benchmark Fixes

### Task 5: Support --flag=value syntax in parseArgs (Finding 8)

**Files:**
- Modify: `main.go:174-211` — parseArgs function

Currently only `--flag value` (space-separated) works. `--log=/tmp/file` is silently treated as prompt text.

- [ ] **Step 1: Add support for = syntax**

Before the switch statement, split args that contain `=`:

```go
func parseArgs(args []string) (...) {
    var parts []string
    for i := 0; i < len(args); i++ {
        arg := args[i]

        // Support --flag=value syntax by splitting on first =
        if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
            eqIdx := strings.Index(arg, "=")
            // Insert the value as a separate arg
            args = append(args[:i+1], append([]string{arg[eqIdx+1:]}, args[i+1:]...)...)
            args[i] = arg[:eqIdx]
            arg = args[i]
        }

        switch arg {
        // ... existing cases unchanged
        }
    }
    // ...
}
```

- [ ] **Step 2: Run build and quick test**

Run: `go build -o nanocode . && echo "OK"`
Manual: `./nanocode --log=/tmp/test_eq.log "hi" && cat /tmp/test_eq.log`

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "fix: support --flag=value syntax in CLI argument parsing"
```

---

### Task 6: Add --help to benchmark CLI (Finding 5)

**Files:**
- Modify: `internal/benchmark/cmd.go`
- Modify: `internal/benchmark/benchmark_test.go`

- [ ] **Step 1: Add help flag handling**

In `ParseCLIArgs`, before the main loop:

```go
func ParseCLIArgs(args []string) (CLIArgs, error) {
    var a CLIArgs
    for _, arg := range args {
        if arg == "--help" || arg == "-h" {
            return a, fmt.Errorf("usage: nanocode benchmark --suite=<path> | --task=<path>")
        }
        switch {
        // ... existing cases
        }
    }
    // ...
}
```

- [ ] **Step 2: Add test**

```go
func TestParseCLIArgsHelp(t *testing.T) {
    _, err := ParseCLIArgs([]string{"--help"})
    if err == nil {
        t.Fatal("expected error for --help")
    }
    if !strings.Contains(err.Error(), "usage:") {
        t.Errorf("expected usage message, got: %v", err)
    }
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/benchmark/ -run "TestParseCLI" -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/benchmark/cmd.go internal/benchmark/benchmark_test.go
git commit -m "fix: add --help flag to benchmark CLI subcommand"
```

---

### Task 7: Disable snapshot manager for benchmark runs (Finding 4)

**Files:**
- Modify: `main.go:271-277` — benchmark engine factory

The benchmark factory creates engines with `workCfg.ProjectDir = workDir` but temp dirs aren't git repos, causing `git add` errors in snapshot manager.

- [ ] **Step 1: Set ProjectDir to empty for benchmark engines**

In the factory function, don't set ProjectDir so the snapshot manager won't be created:

Actually wait — the engine needs ProjectDir to resolve relative paths for tool operations. The real fix: the snapshot manager is only created when `baseDir != ""` (engine.go:83). We need to ensure benchmark engines don't create snapshot managers. The simplest fix: add a config flag to disable snapshots.

Actually, the simplest approach: just don't initialize git in temp dirs. The snapshot manager already handles non-git dirs gracefully (logs and returns). The issue is just noisy log output. Let's suppress the log output instead by checking if the dir is a git repo before running git commands.

Actually the cleanest fix: skip snapshot for benchmark by leaving ProjectDir empty and using a separate WorkDir for tool path resolution. But that's a bigger change.

Simplest fix that addresses the issue: add `DisableSnapshot bool` to config, set it in benchmark factory.

In `internal/config/config.go`, add:
```go
DisableSnapshot bool `json:"-"` // internal-only, not serialized
```

In `internal/engine/engine.go`, check it:
```go
if baseDir != "" && !cfg.DisableSnapshot {
    snapMgr = snapshot.New(baseDir, s)
    onChange = snapMgr.Track
}
```

In `main.go` benchmark factory:
```go
workCfg.DisableSnapshot = true
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/engine/ -v && go build -o nanocode .`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go internal/engine/engine.go main.go
git commit -m "fix: disable snapshot manager for benchmark runs to suppress git errors"
```

---

### Task 8: Add config.json to benchmark tasks (Finding 7)

**Files:**
- Create: `benchmarks/simple-edits/001-fix-typo/config.json`
- Create: `benchmarks/simple-edits/002-add-comment/config.json`
- Create: `benchmarks/simple-edits/003-change-string/config.json`
- Create: `benchmarks/file-operations/001-create-file/config.json`
- Create: `benchmarks/file-operations/002-edit-file/config.json`
- Create: `benchmarks/file-operations/003-create-nested/config.json`
- Create: `benchmarks/file-operations/004-append-to-file/config.json`
- Create: `benchmarks/file-operations/005-rename-variable/config.json`
- Create: `benchmarks/debugging/001-fix-syntax/config.json`
- Create: `benchmarks/debugging/002-fix-import/config.json`

- [ ] **Step 1: Create config.json files for all 10 tasks**

Each config.json specifies `category` and `expected_tools`. Need to read each task's prompt.txt to determine expected tools.

- [ ] **Step 2: Run loader tests**

Run: `go test ./internal/benchmark/ -run "TestLoad" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add benchmarks/
git commit -m "fix: add config.json with expected_tools to all benchmark tasks"
```

---

### Task 9: Make benchmark verify scripts more robust (Finding 3)

**Files:**
- Modify: `benchmarks/simple-edits/001-fix-typo/verify.sh`

The current verify.sh uses exact string match: `[ "$(cat greeting.txt)" = "Hello, World!" ]`. This fails if the model adds a trailing newline or extra whitespace.

- [ ] **Step 1: Use grep for verification**

```bash
grep -q "Hello, World!" greeting.txt && ! grep -q "Helo" greeting.txt
```

This checks that the typo was fixed and the correct text is present, without being brittle about trailing content.

- [ ] **Step 2: Commit**

```bash
git add benchmarks/simple-edits/001-fix-typo/verify.sh
git commit -m "fix: make benchmark verify scripts more robust against whitespace"
```

---

### Task 10: Add rate-limit retry with backoff to benchmark runner (Finding 6)

**Files:**
- Modify: `internal/benchmark/benchmark.go:100-107`
- Modify: `internal/benchmark/benchmark_test.go`

When the engine returns a 429 error, the benchmark should retry with exponential backoff instead of failing immediately.

- [ ] **Step 1: Add retry logic to RunTask**

Wrap the engine.Run call with retry:

```go
const maxRetries = 3
const initialBackoff = 5 * time.Second

// In RunTask, replace the direct eng.Run call:
var records []ToolCallRecord
var runErr error
for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        backoff := initialBackoff * time.Duration(1<<(attempt-1))
        select {
        case <-time.After(backoff):
        case <-ctx.Done():
            result.Error = fmt.Sprintf("context cancelled during retry: %v", ctx.Err())
            return result
        }
    }
    records, runErr = eng.Run(ctx, task.Prompt)
    if runErr == nil || !strings.Contains(runErr.Error(), "429") {
        break
    }
}
```

- [ ] **Step 2: Add test for retry behavior**

```go
func TestRunTaskRetries429(t *testing.T) {
    attempts := 0
    runner := &Runner{
        EngineFactory: func(_ string) (EngineRunner, error) {
            return &mockRetryEngine{attempts: &attempts, failUntil: 2}, nil
        },
    }
    task := Task{ID: "retry-test", Prompt: "test", VerifyScript: "exit 0"}
    result := runner.RunTask(context.Background(), task)
    if !result.Passed {
        t.Errorf("expected passed after retries, got error: %s", result.Error)
    }
    if attempts != 3 { // 2 failures + 1 success
        t.Errorf("attempts = %d, want 3", attempts)
    }
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/benchmark/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/benchmark/benchmark.go internal/benchmark/benchmark_test.go
git commit -m "fix: add rate-limit retry with exponential backoff to benchmark runner"
```

---

## Chunk 3: Finding 1 (test guide fix)

Finding 1 (test guide uses `/tmp/` paths outside project boundary) is in a GitHub issue comment, not in code. Cannot fix in code. The actual behavior (blocking writes outside project dir) is correct — it's the guide that's inaccurate. No code change needed.
