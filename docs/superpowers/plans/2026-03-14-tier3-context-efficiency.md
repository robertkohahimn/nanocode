# Tier 3: Context & Efficiency Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement checkpoint reminders (T3.5), project context injection (T3.2), and automatic context summarization (T3.1) for the nanocode engine.

**Architecture:** Three independent harnesses layered onto the engine loop. Checkpoint reminders inject escalating progress check-ins. Project context enriches the system prompt with git/env info. Context summarization replaces the hard 40-message window with LLM-generated summaries, falling back to windowing on error.

**Tech Stack:** Go, `os/exec` for git commands, existing provider interface for summarization LLM calls.

**Spec:** `docs/superpowers/specs/2026-03-14-tier3-context-efficiency-design.md`

---

## Chunk 1: T3.5 — Checkpoint Verification Reminders

### Task 1: Add CheckpointInterval to config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add config field**

In `internal/config/config.go`, add `CheckpointInterval` to the `Config` struct after `DisableVerification`:

```go
CheckpointInterval   int       `json:"checkpointInterval"`    // 0 = disabled
```

- [ ] **Step 2: Add merge logic**

In the `merge` function, add after the `DisableVerification` block:

```go
if overlay.CheckpointInterval != 0 {
    base.CheckpointInterval = overlay.CheckpointInterval
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/...`
Expected: PASS (no config tests exist, but verify compilation)

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add CheckpointInterval field for T3.5"
```

### Task 2: Implement CheckpointInjector

**Files:**
- Create: `internal/engine/checkpoint.go`
- Create: `internal/engine/checkpoint_test.go`

- [ ] **Step 1: Write checkpoint_test.go**

```go
package engine

import (
	"strings"
	"testing"
)

func TestCheckpointInjectorGentle(t *testing.T) {
	ci := NewCheckpointInjector(10)
	msg, level := ci.MaybeInject(10)
	if msg == nil {
		t.Fatal("expected checkpoint at iteration 10")
	}
	if level != "gentle" {
		t.Errorf("expected gentle level, got %q", level)
	}
	if msg.Type != "text" {
		t.Errorf("expected text block, got %q", msg.Type)
	}
	if !strings.Contains(msg.Text, "Checkpoint") {
		t.Error("expected checkpoint text")
	}
	if !strings.Contains(msg.Text, "accomplished") {
		t.Error("expected gentle tone at iteration 10")
	}
}

func TestCheckpointInjectorWarning(t *testing.T) {
	ci := NewCheckpointInjector(10)
	msg, level := ci.MaybeInject(30)
	if msg == nil {
		t.Fatal("expected checkpoint at iteration 30")
	}
	if level != "warning" {
		t.Errorf("expected warning level, got %q", level)
	}
	if !strings.Contains(msg.Text, "halfway") || !strings.Contains(msg.Text, "stuck") {
		t.Error("expected warning tone at iteration 30")
	}
}

func TestCheckpointInjectorUrgent(t *testing.T) {
	ci := NewCheckpointInjector(10)
	msg, level := ci.MaybeInject(40)
	if msg == nil {
		t.Fatal("expected checkpoint at iteration 40")
	}
	if level != "urgent" {
		t.Errorf("expected urgent level, got %q", level)
	}
	if !strings.Contains(msg.Text, "stopping") {
		t.Error("expected urgent tone at iteration 40")
	}
}

func TestCheckpointInjectorNoFireBetween(t *testing.T) {
	ci := NewCheckpointInjector(10)
	for _, iter := range []int{0, 1, 5, 9, 11, 15, 19} {
		if msg, _ := ci.MaybeInject(iter); msg != nil {
			t.Errorf("unexpected checkpoint at iteration %d", iter)
		}
	}
}

func TestCheckpointInjectorDisabled(t *testing.T) {
	ci := NewCheckpointInjector(0)
	for _, iter := range []int{10, 20, 25, 30, 40} {
		if msg, _ := ci.MaybeInject(iter); msg != nil {
			t.Errorf("checkpoint should be disabled, fired at iteration %d", iter)
		}
	}
}

func TestCheckpointInjectorCustomInterval(t *testing.T) {
	ci := NewCheckpointInjector(5)
	if msg, _ := ci.MaybeInject(5); msg == nil {
		t.Error("expected checkpoint at iteration 5 with interval=5")
	}
	if msg, _ := ci.MaybeInject(4); msg != nil {
		t.Error("unexpected checkpoint at iteration 4")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestCheckpointInjector -v`
Expected: FAIL — `NewCheckpointInjector` undefined

- [ ] **Step 3: Implement checkpoint.go**

```go
package engine

import (
	"fmt"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// CheckpointInjector injects periodic progress reminders into the conversation.
// Reminders escalate in urgency as iterations increase.
type CheckpointInjector struct {
	interval int // 0 = disabled
}

// NewCheckpointInjector creates a CheckpointInjector with the given interval.
// An interval of 0 disables checkpoint injection.
func NewCheckpointInjector(interval int) *CheckpointInjector {
	return &CheckpointInjector{interval: interval}
}

// MaybeInject returns a checkpoint content block and its urgency level if one
// should be injected at the given iteration. Returns nil, "" otherwise.
func (ci *CheckpointInjector) MaybeInject(iteration int) (*provider.ContentBlock, string) {
	if ci.interval <= 0 || iteration == 0 {
		return nil, ""
	}
	if iteration%ci.interval != 0 {
		return nil, ""
	}
	level, body := ci.escalation(iteration)
	text := fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>", body)
	return &provider.ContentBlock{Type: "text", Text: text}, level
}

// escalation returns the urgency level and message body for a given iteration.
func (ci *CheckpointInjector) escalation(iteration int) (string, string) {
	switch {
	case iteration >= 40:
		return "urgent", fmt.Sprintf(
			"Checkpoint (iteration %d of %d):\n"+
				"You have used most of your iteration budget.\n"+
				"Consider stopping and reporting partial progress.\n"+
				"If stuck, ask the user for help rather than continuing to iterate.",
			iteration, maxIterations)
	case iteration >= 25:
		return "warning", fmt.Sprintf(
			"Checkpoint (iteration %d of %d):\n"+
				"You're halfway through your iteration budget.\n"+
				"- Are you stuck? If so, try a different approach.\n"+
				"- Are you making progress? If so, what's remaining?",
			iteration, maxIterations)
	default:
		return "gentle", fmt.Sprintf(
			"Checkpoint (iteration %d of %d):\n"+
				"- What have you accomplished so far?\n"+
				"- What's remaining?\n"+
				"- Are you on track or stuck?\n\n"+
				"If stuck, consider: asking for help, trying a different approach, or simplifying the goal.",
			iteration, maxIterations)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestCheckpointInjector -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/checkpoint.go internal/engine/checkpoint_test.go
git commit -m "feat(engine): add checkpoint verification reminders (T3.5)"
```

### Task 3: Add LogCheckpoint to logger

**Files:**
- Modify: `internal/engine/logger.go`

- [ ] **Step 1: Add checkpoint entry type and log method**

Add after the `sessionEndEntry` struct (line 62):

```go
// checkpointEntry records a checkpoint injection.
type checkpointEntry struct {
	baseEntry
	Iteration int    `json:"iteration"`
	Level     string `json:"level"`
}
```

Add after `LogSessionEnd` (after line 146):

```go
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
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/logger.go
git commit -m "feat(engine): add LogCheckpoint to structured logger"
```

### Task 4: Wire checkpoint into engine loop

**Files:**
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: Create checkpoint injector in loop**

In `engine.go`, in the `loop` function, after `verifyState := &VerifyState{}` (line 299), add:

```go
checkpoint := NewCheckpointInjector(cfg.CheckpointInterval)
```

- [ ] **Step 2: Inject checkpoint before LLM call**

The checkpoint should fire at the START of each iteration, before the LLM call. Add after line 304 (`if ctx.Err() != nil { return ctx.Err() }`):

```go
if cb, level := checkpoint.MaybeInject(iterations); cb != nil {
	logger.LogCheckpoint(iterations, level)
	messages = append(messages, provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.ContentBlock{*cb},
	})
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go
git commit -m "feat(engine): wire checkpoint reminders into loop (T3.5 complete)"
```

---

## Chunk 2: T3.2 — Enhanced Project Context Injection

### Task 5: Implement BuildProjectContext

**Files:**
- Create: `internal/engine/context.go`
- Create: `internal/engine/context_test.go`

- [ ] **Step 1: Write context_test.go**

```go
package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProjectContextGitRepo(t *testing.T) {
	dir := t.TempDir()

	// Initialize git repo with a commit
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
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644)
	run("add", "hello.txt")
	run("commit", "-m", "initial commit")

	ctx := BuildProjectContext(dir)

	if !strings.Contains(ctx, "main") {
		t.Error("expected branch name 'main' in context")
	}
	if !strings.Contains(ctx, "initial commit") {
		t.Error("expected commit message in context")
	}
	if !strings.Contains(ctx, "Working directory:") {
		t.Error("expected working directory in context")
	}
}

func TestBuildProjectContextNonGitDir(t *testing.T) {
	dir := t.TempDir()
	ctx := BuildProjectContext(dir)

	// Should still have environment info
	if !strings.Contains(ctx, "Working directory:") {
		t.Error("expected working directory even without git")
	}
	// Should NOT have git info
	if strings.Contains(ctx, "Current branch:") {
		t.Error("should not have branch info for non-git directory")
	}
}

func TestBuildProjectContextWithNanocodeMd(t *testing.T) {
	dir := t.TempDir()
	content := "# Project Instructions\nDo the thing."
	os.WriteFile(filepath.Join(dir, "nanocode.md"), []byte(content), 0o644)

	ctx := BuildProjectContext(dir)

	if !strings.Contains(ctx, "Do the thing.") {
		t.Error("expected nanocode.md content in context")
	}
}

func TestBuildProjectContextEmptyDir(t *testing.T) {
	ctx := BuildProjectContext("")
	// Empty project dir should return empty
	if ctx != "" {
		t.Errorf("expected empty context for empty dir, got %q", ctx)
	}
}

func TestBuildProjectContextStatusTruncation(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		cmd.CombinedOutput()
	}
	run("init")
	run("checkout", "-b", "main")

	// Create many untracked files to produce long status output
	for i := 0; i < 30; i++ {
		os.WriteFile(filepath.Join(dir, strings.Repeat("f", 10)+string(rune('a'+i%26))+".txt"), []byte("x"), 0o644)
	}

	ctx := BuildProjectContext(dir)

	// Count lines with "??" (untracked marker in git status --short)
	lines := strings.Split(ctx, "\n")
	statusLines := 0
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "??") {
			statusLines++
		}
	}
	if statusLines > 20 {
		t.Errorf("expected status truncated to 20 lines, got %d", statusLines)
	}
}

func TestBuildProjectContextNoCommits(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s (%v)", out, err)
	}

	ctx := BuildProjectContext(dir)

	// Should have environment info and branch, but no commits section
	if !strings.Contains(ctx, "Working directory:") {
		t.Error("expected working directory")
	}
	// git log with no commits returns empty — should not crash
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestBuildProjectContext -v`
Expected: FAIL — `BuildProjectContext` undefined

- [ ] **Step 3: Implement context.go**

```go
package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	maxStatusLines = 20
	gitTimeout     = 2 * time.Second
	maxProjectCtx  = 1 << 20 // 1MB
)

// BuildProjectContext builds a system prompt suffix with git info,
// environment details, and nanocode.md content for the given project directory.
// Returns an empty string if projectDir is empty.
func BuildProjectContext(projectDir string) string {
	if projectDir == "" {
		return ""
	}

	var sb strings.Builder

	// Git info (only if inside a git repo)
	if isGitRepo(projectDir) {
		if branch := gitCommand(projectDir, "branch", "--show-current"); branch != "" {
			sb.WriteString("Current branch: ")
			sb.WriteString(branch)
			sb.WriteString("\n\n")
		}

		if status := gitCommand(projectDir, "status", "--short"); status != "" {
			lines := strings.Split(status, "\n")
			if len(lines) > maxStatusLines {
				lines = append(lines[:maxStatusLines], fmt.Sprintf("... (%d more)", len(lines)-maxStatusLines))
			}
			sb.WriteString("Status:\n")
			sb.WriteString(strings.Join(lines, "\n"))
			sb.WriteString("\n\n")
		}

		if commits := gitCommand(projectDir, "log", "--oneline", "-5"); commits != "" {
			sb.WriteString("Recent commits:\n")
			sb.WriteString(commits)
			sb.WriteString("\n\n")
		}
	}

	// Environment
	sb.WriteString(fmt.Sprintf("Working directory: %s\n", projectDir))
	sb.WriteString(fmt.Sprintf("Platform: %s\n", runtime.GOOS))
	sb.WriteString(fmt.Sprintf("Date: %s\n", time.Now().Format("2006-01-02")))

	// Project instructions (nanocode.md)
	nanocodePath := filepath.Join(projectDir, "nanocode.md")
	if f, err := os.Open(nanocodePath); err == nil {
		data, readErr := io.ReadAll(io.LimitReader(f, maxProjectCtx+1))
		f.Close()
		if readErr == nil && len(data) > 0 {
			content := string(data)
			if len(data) > maxProjectCtx {
				content = content[:maxProjectCtx] + "\n... (truncated at 1MB)"
			}
			sb.WriteString("\n")
			sb.WriteString(content)
		}
	}

	return sb.String()
}

// isGitRepo checks if the directory is inside a git repository.
func isGitRepo(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// gitCommand runs a git command in the given directory and returns
// trimmed stdout. Returns empty string on any error.
func gitCommand(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestBuildProjectContext -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/context.go internal/engine/context_test.go
git commit -m "feat(engine): add BuildProjectContext for project context injection (T3.2)"
```

### Task 6: Wire BuildProjectContext into engine loop

**Files:**
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: Replace nanocode.md injection with BuildProjectContext**

In `engine.go`, replace the entire block from line 268 to line 282:

```go
// Auto-read project context file (nanocode.md) if it exists (bounded to 1MB).
if cfg.ProjectDir != "" {
    if f, err := os.Open(filepath.Join(cfg.ProjectDir, "nanocode.md")); err == nil {
        const maxProjectCtx = 1 << 20 // 1MB
        data, readErr := io.ReadAll(io.LimitReader(f, maxProjectCtx+1))
        f.Close()
        if readErr == nil && len(data) > 0 {
            content := string(data)
            if len(data) > maxProjectCtx {
                content = content[:maxProjectCtx] + "\n... (truncated at 1MB)"
            }
            system += "\n\n# Project Context\n\n" + content
        }
    }
}
```

With:

```go
if projectCtx := BuildProjectContext(cfg.ProjectDir); projectCtx != "" {
    system += "\n\n# Project Context\n\n" + projectCtx
}
```

- [ ] **Step 2: Remove unused imports**

The `io` and `filepath` imports in engine.go may now be unused (check if they're used elsewhere in the file). `filepath` is still used for `filepath.Clean` and `filepath.Join` in the tool execution section. `io` is still used for `io.Closer`. Keep both.

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go
git commit -m "feat(engine): wire BuildProjectContext into loop, replace nanocode.md block (T3.2 complete)"
```

---

## Chunk 3: T3.1 — Automatic Context Summarization

### Task 7: Add summarization config fields

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add config fields**

In `config.go`, add after `CheckpointInterval`:

```go
SummarizeThreshold  int `json:"summarizeThreshold"`  // 0 = disabled (use windowing)
SummarizeKeepRecent int `json:"summarizeKeepRecent"` // messages to keep unsummarized
```

- [ ] **Step 2: Add merge logic**

In the `merge` function, add:

```go
if overlay.SummarizeThreshold != 0 {
    base.SummarizeThreshold = overlay.SummarizeThreshold
}
if overlay.SummarizeKeepRecent != 0 {
    base.SummarizeKeepRecent = overlay.SummarizeKeepRecent
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add SummarizeThreshold and SummarizeKeepRecent fields for T3.1"
```

### Task 8: Add LogSummarization to logger

**Files:**
- Modify: `internal/engine/logger.go`

- [ ] **Step 1: Add summarization entry type and log method**

Add after the `checkpointEntry` struct:

```go
// summarizationEntry records a context summarization event.
type summarizationEntry struct {
	baseEntry
	OriginalMessages int `json:"original_messages"`
	ResultMessages   int `json:"result_messages"`
}
```

Add after `LogCheckpoint`:

```go
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
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/logger.go
git commit -m "feat(engine): add LogSummarization to structured logger"
```

### Task 9: Implement Summarizer

**Files:**
- Create: `internal/engine/summarize.go`
- Create: `internal/engine/summarize_test.go`

- [ ] **Step 1: Write summarize_test.go**

```go
package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

func makeMsgs(n int) []provider.Message {
	msgs := make([]provider.Message, n)
	for i := range msgs {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		msgs[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg%d", i)}},
		}
	}
	return msgs
}

func TestSummarizerBelowThreshold(t *testing.T) {
	s := NewSummarizer(nil, 30, 10)
	msgs := makeMsgs(20)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 20 {
		t.Errorf("expected 20 messages unchanged, got %d", len(result))
	}
}

func TestSummarizerDisabled(t *testing.T) {
	s := NewSummarizer(nil, 0, 10)
	msgs := makeMsgs(50)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 50 {
		t.Errorf("expected 50 messages unchanged when disabled, got %d", len(result))
	}
}

func TestSummarizerTriggersAboveThreshold(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Summary: files were edited."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, 30, 10)
	msgs := makeMsgs(35)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Should be: first message + summary message + 10 recent = 12
	if len(result) != 12 {
		t.Errorf("expected 12 messages, got %d", len(result))
	}

	// First message preserved
	if result[0].Content[0].Text != "msg0" {
		t.Errorf("expected first message preserved, got %q", result[0].Content[0].Text)
	}

	// Second message is summary
	if !strings.Contains(result[1].Content[0].Text, "<context-summary>") {
		t.Error("expected summary message with <context-summary> tags")
	}
	if !strings.Contains(result[1].Content[0].Text, "Summary: files were edited.") {
		t.Error("expected summary content from provider")
	}

	// Last message is the original last message
	if result[11].Content[0].Text != "msg34" {
		t.Errorf("expected last recent message to be msg34, got %q", result[11].Content[0].Text)
	}
}

func TestSummarizerFallbackOnError(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventError, Error: fmt.Errorf("API error")},
			},
		},
	}
	s := NewSummarizer(mp, 30, 10)
	msgs := makeMsgs(50) // >40 so windowMessages actually truncates
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal("should not return error, should fall back to windowing")
	}
	// Should fall back to windowMessages (maxContextMessages=40)
	if len(result) > maxContextMessages+1 { // +1 for tool_result pair adjustment
		t.Errorf("expected windowed result on provider failure, got %d messages", len(result))
	}
}

func TestSummarizerPreservesRecentMessages(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Summarized."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, 30, 10)
	msgs := makeMsgs(40)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Last 10 messages should be msgs[30..39]
	for i := 0; i < 10; i++ {
		expected := fmt.Sprintf("msg%d", 30+i)
		actual := result[2+i].Content[0].Text
		if actual != expected {
			t.Errorf("recent[%d]: expected %q, got %q", i, expected, actual)
		}
	}
}

func TestSummarizerWithExistingSummary(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Re-summarized."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, 10, 5)

	// Build messages where msg[1] is a previous summary
	msgs := makeMsgs(15)
	msgs[1] = provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: "<context-summary>\nPrevious summary content.\n</context-summary>",
		}},
	}

	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Should still produce a valid result
	if len(result) != 7 { // first + summary + 5 recent
		t.Errorf("expected 7 messages, got %d", len(result))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestSummarizer -v`
Expected: FAIL — `NewSummarizer` undefined

- [ ] **Step 3: Implement summarize.go**

```go
package engine

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

const summaryPrompt = `Summarize this conversation segment concisely:
- Key decisions made
- Files read/modified
- Errors encountered and how resolved
- Current state of the task

Keep technical details. Omit pleasantries.`

const maxSummaryInput = 100 * 1024 // 100KB

// Summarizer compresses old conversation messages using the LLM provider.
type Summarizer struct {
	provider  provider.Provider
	threshold int // message count to trigger summarization (0 = disabled)
	keepN     int // number of recent messages to keep unsummarized
}

// NewSummarizer creates a Summarizer. If threshold is 0, summarization is disabled.
func NewSummarizer(p provider.Provider, threshold, keepN int) *Summarizer {
	if keepN <= 0 {
		keepN = 10
	}
	return &Summarizer{provider: p, threshold: threshold, keepN: keepN}
}

// MaybeSummarize compresses messages if they exceed the threshold.
// On failure, falls back to windowMessages.
func (s *Summarizer) MaybeSummarize(ctx context.Context, messages []provider.Message) ([]provider.Message, error) {
	if s.threshold <= 0 || len(messages) < s.threshold {
		return messages, nil
	}

	first := messages[0]
	keepN := s.keepN
	if keepN >= len(messages)-1 {
		return messages, nil
	}
	middle := messages[1 : len(messages)-keepN]
	recent := messages[len(messages)-keepN:]

	summary, err := s.generateSummary(ctx, middle)
	if err != nil {
		log.Printf("engine: summarization failed, falling back to windowing: %v", err)
		return windowMessages(messages, maxContextMessages), nil
	}

	result := make([]provider.Message, 0, 2+len(recent))
	result = append(result, first)
	result = append(result, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("<context-summary>\n%s\n</context-summary>", summary),
		}},
	})
	result = append(result, recent...)
	return result, nil
}

// generateSummary calls the LLM to summarize a slice of messages.
func (s *Summarizer) generateSummary(ctx context.Context, messages []provider.Message) (string, error) {
	// Format messages for the summary prompt
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] ", msg.Role))
		for _, cb := range msg.Content {
			switch cb.Type {
			case "text":
				sb.WriteString(cb.Text)
			case "tool_use":
				if cb.ToolCall != nil {
					sb.WriteString(fmt.Sprintf("<tool:%s>", cb.ToolCall.Name))
				}
			case "tool_result":
				if cb.ToolResult != nil {
					content := cb.ToolResult.Content
					if len(content) > 500 {
						content = content[:500] + "..."
					}
					sb.WriteString(content)
				}
			}
			sb.WriteByte('\n')
		}
	}

	// Truncate input to bound cost
	input := sb.String()
	if len(input) > maxSummaryInput {
		input = input[:maxSummaryInput] + "\n... (truncated)"
	}

	req := &provider.Request{
		Messages: []provider.Message{
			{
				Role: provider.RoleUser,
				Content: []provider.ContentBlock{{
					Type: "text",
					Text: summaryPrompt + "\n\n---\n\n" + input,
				}},
			},
		},
		MaxTokens: 1024,
		System:    "You are a concise technical summarizer.",
	}

	events, err := s.provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("starting summary stream: %w", err)
	}

	var result strings.Builder
	for ev := range events {
		switch ev.Type {
		case provider.EventTextDelta:
			result.WriteString(ev.Text)
		case provider.EventError:
			return "", fmt.Errorf("summary stream error: %w", ev.Error)
		}
	}

	return result.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestSummarizer -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/summarize.go internal/engine/summarize_test.go
git commit -m "feat(engine): add automatic context summarization (T3.1)"
```

### Task 10: Wire summarization into engine loop

**Files:**
- Modify: `internal/engine/engine.go`

- [ ] **Step 1: Create summarizer in loop**

In `engine.go`, in the `loop` function, after the checkpoint injector creation, add:

```go
summarizer := NewSummarizer(e.provider, cfg.SummarizeThreshold, cfg.SummarizeKeepRecent)
```

- [ ] **Step 2: Replace windowMessages call with MaybeSummarize + fallback**

Replace the windowing block (around line 306-309):

```go
windowed := windowMessages(messages, maxContextMessages)
if len(windowed) < len(messages) {
    logger.LogContextWindow(len(messages), len(windowed))
}
```

With:

```go
// Summarize if enabled, then apply windowing as safety net.
summarized, sumErr := summarizer.MaybeSummarize(ctx, messages)
if sumErr != nil {
    log.Printf("engine: summarization error: %v", sumErr)
    summarized = messages
}
if len(summarized) < len(messages) {
    logger.LogSummarization(len(messages), len(summarized))
}
windowed := windowMessages(summarized, maxContextMessages)
if len(windowed) < len(summarized) {
    logger.LogContextWindow(len(summarized), len(windowed))
}
```

- [ ] **Step 3: Run all engine tests**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/engine.go
git commit -m "feat(engine): wire summarization into loop with windowing fallback (T3.1 complete)"
```

---

## Chunk 4: Final Verification

### Task 11: Full test suite and line count verification

- [ ] **Step 1: Run complete test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Verify file line counts**

Run: `wc -l internal/engine/checkpoint.go internal/engine/context.go internal/engine/summarize.go internal/engine/engine.go internal/engine/logger.go internal/config/config.go`

Expected: All files under 500 lines.

- [ ] **Step 3: Verify build produces single binary**

Run: `go build -o /dev/null .`
Expected: Clean build, no errors.

- [ ] **Step 4: Final commit if any cleanup needed**

Only if adjustments were required during verification.
