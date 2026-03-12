# Batch Confirmation for Sequential Shell Commands

**Date:** 2026-03-12
**Issue:** [#7](https://github.com/robertkohahimn/nanocode/issues/7)
**Status:** Approved

## Problem

When the LLM requests multiple shell commands in a single response, users must confirm each one individually:

```
Run: mkdir -p project [Y/n] Y
Run: pwd [Y/n] Y
Run: ls [Y/n] Y
Run: pip install flask [Y/n] Y
```

This creates friction when commands arrive in rapid succession.

## Solution

Present all bash commands requiring confirmation as a numbered batch with a single prompt:

```
Pending commands:
  1. mkdir -p project
  2. pwd
  3. ls
  4. pip install flask

Run all? [Y/n/1,3,4]
```

Users can approve all (`Y`), reject all (`n`), or select specific commands by number.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Batching approach | Preview all upfront | Full visibility before execution |
| Selection model | Numbered (`Y/n/1,3,4`) | Simple, flexible, CLI-native |
| Skipped command result | `"Command skipped (user selected others from batch)"` | Gives LLM context about partial approval |
| Permission integration | Auto-execute allowed, batch the rest | Preserves existing allow-list behavior |
| Single command | Fall back to current prompt | No ceremony for simple case |
| Execution order | Confirm upfront, execute in original order | Preserves LLM's intended sequence |

## Architecture

### Files Modified

| File | Change |
|------|--------|
| `internal/engine/engine.go` | Add batch confirmation before tool execution loop |
| `internal/tool/bash.go` | Export command parsing; add confirmation override support |

### New File

| File | Purpose |
|------|---------|
| `internal/engine/batch_confirm.go` | Batch prompt UI and input parsing (~80 lines) |

### Flow

```
LLM Response arrives
       ↓
collectResponse() extracts toolCalls slice
       ↓
┌─────────────────────────────────────────┐
│  collectBashConfirmations()             │
│  1. Filter bash tool calls              │
│  2. Parse command from each             │
│  3. Check permission (allow-list)       │
│  4. Collect commands needing confirm    │
│  5. If count > 1: show batch prompt     │
│  6. If count == 1: return nil (fallback)│
│  7. Store decisions in map[toolCallID]  │
└─────────────────────────────────────────┘
       ↓
for _, tc := range toolCalls {
    // BashTool checks override map first
    // Other tools execute normally
    result := e.tools.Execute(ctx, tc)
}
```

## Data Structures

### Confirmation Decision

```go
type batchDecision struct {
    approved bool
    skipped  bool   // true if user selected others but not this one
}

type confirmationOverrides map[string]batchDecision
```

### BashTool Enhancement

```go
type BashTool struct {
    ConfirmFunc       func(command string) bool
    confirmOverrides  map[string]batchDecision  // set by engine before execution
    stdinReader       *bufio.Reader
}

func (t *BashTool) SetConfirmOverrides(overrides map[string]batchDecision)
func (t *BashTool) ClearConfirmOverrides()
```

### Exported Input Parsing

```go
type BashInput struct {
    Command string `json:"command"`
}

func ParseBashInput(raw json.RawMessage) (BashInput, error)
```

## User Interface

### Batch Prompt

```
Pending commands:
  1. mkdir -p project
  2. pwd
  3. ls
  4. pip install flask

Run all? [Y/n/1,3,4]
```

### Input Parsing

| Input | Meaning |
|-------|---------|
| `Y`, `y`, `` (empty) | Approve all |
| `N`, `n` | Reject all |
| `1,3,4` | Approve specific (comma-separated) |
| `1 3 4` | Approve specific (space-separated) |
| `1-3` | Approve range |
| Invalid | Re-prompt with error |

### Colors

- Yellow (`\033[33m`) for "Pending commands:" header
- Dim (`\033[2m`) for `[Y/n/1,3,4]` hint

## Error Handling

| Scenario | Handling |
|----------|----------|
| All commands allow-listed | No prompt, all auto-execute |
| Invalid selection input | Re-prompt: `Invalid selection. Enter Y, n, or numbers 1-4` |
| Context cancelled | Return immediately, all rejected |
| Empty tool calls | No-op |

### Tool Result Messages

| Decision | Result |
|----------|--------|
| Approved & executed | Normal output |
| Skipped | `"Command skipped (user selected others from batch)"` |
| Rejected | `"Command rejected by user"` |

## Testing

### batch_confirm_test.go

- `TestParseSelection_All` - Y/y/empty approves all
- `TestParseSelection_None` - N/n rejects all
- `TestParseSelection_Numbers` - comma-separated indices
- `TestParseSelection_Spaces` - space-separated indices
- `TestParseSelection_Range` - range syntax (1-3)
- `TestParseSelection_Invalid` - error on bad input
- `TestPromptBatch_Integration` - mock stdin, verify output

### bash_test.go Updates

- `TestBashTool_ConfirmOverride_Approved` - override skips prompt
- `TestBashTool_ConfirmOverride_Skipped` - returns skip message
- `TestBashTool_NoOverride_FallsBack` - uses ConfirmFunc

### engine_test.go Updates

- `TestEngine_BatchConfirm_MultipleBash` - batch prompt for 3+ bash
- `TestEngine_BatchConfirm_SingleBash` - fallback for single
- `TestEngine_BatchConfirm_MixedTools` - ordering preserved

## Estimates

- **New code:** ~120 lines
- **Test code:** ~150 lines
- **Files changed:** 3
