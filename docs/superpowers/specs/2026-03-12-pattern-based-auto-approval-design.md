# Pattern-Based Auto-Approval for Safe Commands

**Date:** 2026-03-12
**Issue:** [#6](https://github.com/robertkohahimn/nanocode/issues/6)
**Status:** Approved

## Summary

Extend the permission system to support glob-pattern-based auto-approval of safe bash commands, reducing confirmation fatigue while maintaining security for destructive operations.

## Goals

- Skip Y/n confirmation for safe, read-only commands matching user-defined patterns
- Maintain security: deny patterns and meta-commands always blocked
- User-configurable via `nanocode.json`
- Provide `--strict` flag to disable auto-approval when needed

## Non-Goals

- Complex glob syntax (only `*` wildcard supported)
- Per-session or per-directory auto-approve overrides
- Learning/suggesting patterns based on user behavior

## Design Decisions

1. **autoApprove implies allow** — Commands matching autoApprove don't need separate allow-list entries
2. **Simple glob matching** — Only `*` wildcard (matches any characters), no `?`, `[abc]`, or `**`
3. **Extend existing Checker** — Single point of truth for command validation via `CheckResult` struct

## Config Structure

```json
{
  "tools": {
    "bash": {
      "deny": ["rm *", "sudo *"],
      "autoApprove": [
        "ls *",
        "pwd",
        "cat *",
        "head *",
        "tail *",
        "git status",
        "git diff *",
        "go build *",
        "go test *"
      ]
    }
  }
}
```

## API Changes

### permission.go

```go
// CheckResult contains the outcome of command validation.
type CheckResult struct {
    Allowed     bool   // passes allow/deny checks
    AutoApprove bool   // matches autoApprove pattern, skip confirmation
    Reason      string // explanation if not allowed
}

// NewChecker creates a permission checker.
func NewChecker(allow, deny, autoApprove []string) *Checker

// Check validates a command and returns the result.
func (c *Checker) Check(command string) CheckResult

// matchGlob checks if text matches a simple glob pattern.
// Only * is supported as wildcard (matches any characters).
func matchGlob(pattern, text string) bool
```

### config.go

```go
type ToolConfig struct {
    Allow       []string `json:"allow"`
    Deny        []string `json:"deny"`
    AutoApprove []string `json:"autoApprove"`
}

type Config struct {
    // ... existing fields ...
    StrictMode bool `json:"-"` // CLI-only, disables auto-approval
}
```

### main.go

New flag: `--strict` — disables auto-approval, all commands require confirmation.

## Precedence Rules

Commands are evaluated in this order:

1. **Meta-commands** (`eval`, `exec`, `bash -c`, `sh -c`) — always blocked
2. **Wrapper commands** (`sudo`, `env`, `xargs`, `find`, etc.) — always blocked
3. **Deny patterns** — if command matches any deny pattern, blocked
4. **AutoApprove patterns** — if ALL commands in pipeline match and not `--strict`, auto-approve
5. **Allow list** — if set, command must be in allow list
6. **Default** — prompt for Y/n confirmation

## Pipeline Behavior

For pipelines like `ls | grep foo`:
- **Deny:** If ANY command matches a deny pattern, the entire pipeline is blocked
- **AutoApprove:** ALL commands must match an autoApprove pattern for auto-approval
- **Allow:** ALL commands must be in the allow list (if set)

## User Experience

```
# Blocked command (red)
Blocked: "rm -rf /" is in the deny list

# Auto-approved command (green)
Auto-approved: ls -la

# Normal confirmation (yellow)
Run: git push origin main [Y/n]
```

## Files Modified

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `AutoApprove` to `ToolConfig`, `StrictMode` to `Config` |
| `internal/permission/permission.go` | Add `CheckResult`, `matchGlob()`, update `Check()` signature |
| `internal/engine/engine.go` | Check `AutoApprove` and `StrictMode`, show appropriate messages |
| `main.go` | Add `--strict` flag parsing |
| `internal/config/config_test.go` | Test autoApprove parsing and merge behavior |
| `internal/permission/permission_test.go` | Test glob matching and auto-approve logic |

## Testing Strategy

### Glob Matching Tests
- `*` matches empty string, single word, multiple words
- `ls *` matches `ls`, `ls -la`, `ls foo bar`
- `git status` matches exactly, not `git commit`
- Pattern without `*` is exact match

### AutoApprove Behavior Tests
- Single command matching autoApprove returns `AutoApprove=true`
- Pipeline where all commands match returns `AutoApprove=true`
- Pipeline where one command doesn't match returns `AutoApprove=false`
- Command matching both deny and autoApprove returns `Allowed=false`
- autoApprove implies allow (no explicit allow list needed)

### Existing Tests
- All existing allow/deny tests continue to pass
- Meta-commands blocked regardless of autoApprove patterns

## Security Considerations

- Deny always takes precedence over autoApprove
- Meta-commands and wrapper commands are hardcoded blocks, not configurable
- Variable expansion in commands is blocked (cannot validate statically)
- `--strict` flag provides escape hatch for sensitive operations
