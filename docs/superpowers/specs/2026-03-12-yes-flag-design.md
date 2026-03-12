# Design: `--yes` Flag for Auto-Confirm Mode

**Issue:** #5 — Feature: Add --yes flag for auto-confirm mode
**Date:** 2026-03-12
**Status:** Approved

## Summary

Add `--yes` / `-y` command-line flags that automatically approve all bash command confirmations, enabling scripted/automated usage, CI/CD pipelines, and power users who trust the model's command choices.

## CLI Interface

**Flags:** `--yes` and `-y` (both equivalent, boolean, no value required)

**Behavior:**
- When present, prints warning to stderr at startup
- Bypasses interactive Y/n prompts for bash commands
- Security blocks (eval, exec, sudo, etc.) remain enforced

**Warning message:**
```text
⚠️  Auto-confirm enabled: all shell commands will run without confirmation
```

**`parseArgs` signature:**
```go
func parseArgs(args []string) (prompt, sessionID string, listMode bool, modelOverride string, autoConfirm bool)
```

## Engine Integration

**`engine.New` signature:**
```go
func New(p provider.Provider, s store.Store, cfg *config.Config, stdinReader *bufio.Reader, autoConfirm bool) *Engine
```

**Implementation order in `engine.New()`:**
1. Create BashTool with default interactive confirm
2. If `autoConfirm`, replace ConfirmFunc with always-true function
3. Permission wrapper applies afterward (preserves security blocks)

```go
bashTool := tool.NewBashTool(stdinReader)

if autoConfirm {
    bashTool.ConfirmFunc = func(command string) bool {
        return true
    }
}

// Permission wrapper applies AFTER
if bashCfg, ok := cfg.Tools["bash"]; ok {
    if len(bashCfg.Allow) > 0 || len(bashCfg.Deny) > 0 {
        checker := permission.NewChecker(bashCfg.Allow, bashCfg.Deny)
        origConfirm := bashTool.ConfirmFunc
        bashTool.ConfirmFunc = func(cmd string) bool {
            if err := checker.Check(cmd); err != nil {
                fmt.Fprintf(os.Stderr, "\033[31mBlocked:\033[0m %s\n", err)
                return false
            }
            return origConfirm(cmd)
        }
    }
}
```

**Result:** Permission check runs first; if allowed, auto-approves without prompting.

## Warning Display

**Location:** `main.go` `run()` function, after `parseArgs`, before other operations.

**Output:** `os.Stderr` (consistent with other status messages)

```go
if autoConfirm {
    fmt.Fprintln(os.Stderr, "⚠️  Auto-confirm enabled: all shell commands will run without confirmation")
}
```

Warning appears once at startup, not per-command.

## Security Considerations

Commands blocked by the permission system remain blocked even with `--yes`:
- `eval` — bypasses command validation
- `exec` — replaces shell process
- Wrapper commands: `command`, `env`, `xargs`, `find`, `nohup`, `timeout`, `nice`, `strace`, `sudo`
- `bash -c` / `sh -c` — arbitrary command execution
- User-configured deny lists

Only the interactive Y/n confirmation prompt is bypassed.

## Edge Cases

**Interaction with other flags:**
- Works alongside `--session`, `--model`, `--list`
- `--yes` with `--list` is valid (warning printed, sessions listed, no commands run)

**Piped/non-interactive mode:**
- Most useful when stdin is not a terminal
- Without `--yes` in piped mode, bash prompts would block
- With `--yes`, piped input works cleanly

**No bash commands executed:**
- Warning still shown at startup (harmless)

## Testing

**`main_test.go` — parseArgs tests:**
- `--yes` sets `autoConfirm = true`
- `-y` sets `autoConfirm = true`
- No flag leaves `autoConfirm = false`
- Flag works combined with other flags and prompt text

**`internal/engine/engine_test.go` — auto-confirm behavior:**
- With `autoConfirm = true`, ConfirmFunc returns true without stdin read
- With `autoConfirm = false`, default behavior unchanged
- Permission blocks still apply even with `autoConfirm = true`

## Files Modified

1. `main.go` — parseArgs, run function, warning output
2. `internal/engine/engine.go` — New() signature and auto-confirm logic
3. `main_test.go` — parseArgs test cases
4. `internal/engine/engine_test.go` — auto-confirm test cases
