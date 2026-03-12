# Pattern-Based Auto-Approval Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add glob-pattern-based auto-approval for safe bash commands, reducing confirmation fatigue while maintaining security.

**Architecture:** Extend the existing `permission.Checker` to return a `CheckResult` struct containing `Allowed`, `AutoApprove`, and `Reason` fields. Add `matchGlob()` for simple `*` wildcard matching. Wire auto-approval into the engine's confirm hook, respecting a new `--strict` CLI flag.

**Tech Stack:** Go standard library only (no new dependencies). Shell parsing via existing `mvdan.cc/sh/v3`.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/permission/permission.go` | Core permission checker with glob matching and CheckResult |
| `internal/permission/permission_test.go` | Tests for glob matching and auto-approve behavior |
| `internal/config/config.go` | Config struct with AutoApprove field and StrictMode |
| `internal/config/config_test.go` | Tests for config parsing |
| `internal/engine/engine.go` | Wire auto-approval into bash confirm hook |
| `main.go` | Parse `--strict` flag |

---

## Chunk 1: Glob Matching and CheckResult

### Task 1: Add matchGlob function with tests

**Files:**
- Modify: `internal/permission/permission.go:1-10` (add after imports)
- Modify: `internal/permission/permission_test.go` (add new test)

- [ ] **Step 1: Write the failing test for matchGlob**

Add to `internal/permission/permission_test.go`:

```go
func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		// Exact match (no wildcard)
		{"pwd", "pwd", true},
		{"pwd", "pwdx", false},
		{"pwd", "xpwd", false},
		{"git status", "git status", true},
		{"git status", "git commit", false},

		// Wildcard at end
		{"ls *", "ls", true},
		{"ls *", "ls -la", true},
		{"ls *", "ls foo bar", true},
		{"ls *", "lsx", false},
		{"git *", "git status", true},
		{"git *", "git commit -m 'msg'", true},

		// Wildcard at start
		{"* --version", "go --version", true},
		{"* --version", "python --version", true},
		{"* --version", "go version", false},

		// Wildcard in middle
		{"git * --dry-run", "git push --dry-run", true},
		{"git * --dry-run", "git pull origin main --dry-run", true},
		{"git * --dry-run", "git push", false},

		// Multiple wildcards
		{"* status *", "git status -s", true},
		{"* * *", "a b c", true},
		{"* * *", "a b", false},

		// Empty pattern/text edge cases
		{"*", "", true},
		{"*", "anything", true},
		{"", "", true},
		{"", "x", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.text)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/permission -run TestMatchGlob`
Expected: FAIL with "undefined: matchGlob"

- [ ] **Step 3: Implement matchGlob function**

Add to `internal/permission/permission.go` after imports (before `wrapperCommands`):

```go
// matchGlob checks if text matches a simple glob pattern.
// Only * is supported as wildcard (matches any sequence of characters).
// Special case: "cmd *" also matches "cmd" (trailing space before * is optional).
func matchGlob(pattern, text string) bool {
	// Empty pattern only matches empty text
	if pattern == "" {
		return text == ""
	}

	// Split pattern by * wildcard
	parts := strings.Split(pattern, "*")

	// No wildcards = exact match
	if len(parts) == 1 {
		return pattern == text
	}

	// Special case: pattern ends with " *" (space-star) and text equals prefix without space.
	// This allows "ls *" to match both "ls" and "ls -la".
	if len(parts) == 2 && parts[1] == "" && strings.HasSuffix(parts[0], " ") {
		trimmedPrefix := strings.TrimSuffix(parts[0], " ")
		if text == trimmedPrefix {
			return true
		}
	}

	// Check prefix (before first *)
	if !strings.HasPrefix(text, parts[0]) {
		return false
	}
	text = text[len(parts[0]):]

	// Check middle parts
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(text, parts[i])
		if idx == -1 {
			return false
		}
		text = text[idx+len(parts[i]):]
	}

	// Check suffix (after last *)
	return strings.HasSuffix(text, parts[len(parts)-1])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/permission -run TestMatchGlob`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/permission/permission.go internal/permission/permission_test.go
git commit -m "feat(permission): add matchGlob for simple wildcard matching

Supports * wildcard that matches any sequence of characters.
Used for pattern-based auto-approval of safe commands.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

### Task 2: Add CheckResult struct and update Check signature

**Files:**
- Modify: `internal/permission/permission.go:31-37` (Checker struct)
- Modify: `internal/permission/permission.go:64-144` (Check method)

- [ ] **Step 1: Write failing test for CheckResult with auto-approve**

Add to `internal/permission/permission_test.go`:

```go
func TestChecker_AutoApprove(t *testing.T) {
	c := NewChecker(nil, nil, []string{"ls *", "pwd", "cat *"})

	tests := []struct {
		cmd         string
		wantAllowed bool
		wantAuto    bool
	}{
		{"ls -la", true, true},
		{"pwd", true, true},
		{"cat /etc/passwd", true, true},
		{"rm -rf /", true, false}, // allowed (no deny), but not auto-approved
		{"git status", true, false},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed != tt.wantAllowed {
			t.Errorf("Check(%q).Allowed = %v, want %v", tt.cmd, result.Allowed, tt.wantAllowed)
		}
		if result.AutoApprove != tt.wantAuto {
			t.Errorf("Check(%q).AutoApprove = %v, want %v", tt.cmd, result.AutoApprove, tt.wantAuto)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/permission -run TestChecker_AutoApprove`
Expected: FAIL (NewChecker signature wrong, Check returns error not CheckResult)

- [ ] **Step 3: Add CheckResult struct and update Checker**

Replace the Checker struct and NewChecker in `internal/permission/permission.go`:

```go
// CheckResult contains the outcome of command validation.
type CheckResult struct {
	Allowed     bool   // passes allow/deny checks
	AutoApprove bool   // matches autoApprove pattern, skip confirmation
	Reason      string // explanation if not allowed
}

// Checker validates shell commands against allow/deny/autoApprove patterns.
// It parses the full bash AST to catch commands in pipes, subshells,
// command substitution, and process substitution.
type Checker struct {
	allow       map[string]bool // nil = allow all (exact match on base name)
	deny        []string        // glob patterns to block
	autoApprove []string        // glob patterns to auto-approve
}

// NewChecker creates a permission checker from allow/deny/autoApprove lists.
// - allow: exact command names permitted (nil = allow all)
// - deny: glob patterns to block
// - autoApprove: glob patterns to skip confirmation (implies allow)
func NewChecker(allow, deny, autoApprove []string) *Checker {
	c := &Checker{
		deny:        deny,
		autoApprove: autoApprove,
	}
	if len(allow) > 0 {
		c.allow = make(map[string]bool, len(allow))
		for _, cmd := range allow {
			if name := canonicalize(cmd); name != "" {
				c.allow[name] = true
			}
		}
	}
	return c
}
```

- [ ] **Step 4: Update Check method to return CheckResult**

Replace the Check method in `internal/permission/permission.go`:

```go
// Check parses a shell command string and validates every command
// against the allow/deny/autoApprove lists. Returns CheckResult with
// Allowed=true if all commands pass, AutoApprove=true if all commands
// match an autoApprove pattern.
func (c *Checker) Check(command string) CheckResult {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return CheckResult{Allowed: false, Reason: fmt.Sprintf("failed to parse command: %v", err)}
	}

	printer := syntax.NewPrinter()
	allAutoApprove := true // assume true, set false if any command doesn't match
	var walkErr string

	syntax.Walk(file, func(node syntax.Node) bool {
		if walkErr != "" {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Extract command name and normalize to base name
		var nameBuf strings.Builder
		printer.Print(&nameBuf, call.Args[0])
		name := nameBuf.String()
		nameBase := canonicalize(name)
		if nameBase == "" || nameBase == "." {
			walkErr = "empty command name"
			return false
		}

		// Reject commands with variable expansion (can't validate statically)
		if containsExpansion(call.Args[0]) {
			walkErr = fmt.Sprintf("command uses variable expansion %q (cannot validate statically)", name)
			return false
		}

		// Meta-command checks: eval, exec always blocked
		if nameBase == "eval" {
			walkErr = fmt.Sprintf("%q is not permitted (bypasses command validation)", nameBase)
			return false
		}
		if nameBase == "exec" {
			walkErr = fmt.Sprintf("%q is not permitted (replaces shell process)", nameBase)
			return false
		}

		// Wrapper commands that can execute arbitrary sub-commands
		if wrapperCommands[nameBase] {
			walkErr = fmt.Sprintf("%q is not permitted (can execute arbitrary commands)", nameBase)
			return false
		}

		// bash -c / sh -c: check first argument
		if (nameBase == "bash" || nameBase == "sh") && len(call.Args) > 1 {
			var argBuf strings.Builder
			printer.Print(&argBuf, call.Args[1])
			if argBuf.String() == "-c" {
				walkErr = fmt.Sprintf("%q with -c is not permitted (arbitrary command execution)", nameBase)
				return false
			}
		}

		// Get full command string for pattern matching
		var fullCmdBuf strings.Builder
		printer.Print(&fullCmdBuf, call)
		fullCmd := strings.TrimSpace(fullCmdBuf.String())

		// Check deny patterns (glob match on full command)
		for _, pattern := range c.deny {
			if matchGlob(pattern, fullCmd) {
				walkErr = fmt.Sprintf("%q matches deny pattern %q", fullCmd, pattern)
				return false
			}
		}

		// Check allow list (exact match on base name)
		if c.allow != nil && !c.allow[nameBase] {
			// Check if autoApprove implies allow
			autoApproveMatch := false
			for _, pattern := range c.autoApprove {
				if matchGlob(pattern, fullCmd) {
					autoApproveMatch = true
					break
				}
			}
			if !autoApproveMatch {
				walkErr = fmt.Sprintf("%q is not in the allow list", nameBase)
				return false
			}
		}

		// Check autoApprove patterns
		cmdAutoApprove := false
		for _, pattern := range c.autoApprove {
			if matchGlob(pattern, fullCmd) {
				cmdAutoApprove = true
				break
			}
		}
		if !cmdAutoApprove {
			allAutoApprove = false
		}

		return true
	})

	if walkErr != "" {
		return CheckResult{Allowed: false, Reason: "blocked: " + walkErr}
	}

	return CheckResult{Allowed: true, AutoApprove: allAutoApprove}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -v ./internal/permission -run TestChecker_AutoApprove`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/permission/permission.go internal/permission/permission_test.go
git commit -m "feat(permission): add CheckResult struct with AutoApprove field

Check() now returns CheckResult{Allowed, AutoApprove, Reason} instead
of error. AutoApprove=true when all commands match autoApprove patterns.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

### Task 3: Update existing tests to use new Check signature

**Files:**
- Modify: `internal/permission/permission_test.go:8-165` (update all existing tests)

- [ ] **Step 1: Update TestChecker_AllowList**

Replace in `internal/permission/permission_test.go`:

```go
func TestChecker_AllowList(t *testing.T) {
	c := NewChecker([]string{"go", "git", "ls"}, nil, nil)

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"go build ./...", false},
		{"git status", false},
		{"ls -la", false},
		{"rm -rf /", true},
		{"curl http://example.com", true},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed == tt.wantErr {
			t.Errorf("Check(%q).Allowed = %v, wantErr %v", tt.cmd, result.Allowed, tt.wantErr)
		}
	}
}
```

- [ ] **Step 2: Update TestChecker_DenyList**

```go
func TestChecker_DenyList(t *testing.T) {
	c := NewChecker(nil, []string{"rm *", "curl *", "wget *"}, nil)

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"go build ./...", false},
		{"ls -la", false},
		{"rm -rf /", true},
		{"curl http://example.com", true},
		{"wget http://example.com", true},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed == tt.wantErr {
			t.Errorf("Check(%q).Allowed = %v, wantErr %v", tt.cmd, result.Allowed, tt.wantErr)
		}
	}
}
```

- [ ] **Step 3: Update TestChecker_AllowAndDeny**

```go
func TestChecker_AllowAndDeny(t *testing.T) {
	// Allow go and git, but deny git (deny takes precedence)
	c := NewChecker([]string{"go", "git"}, []string{"git *"}, nil)

	result := c.Check("go build")
	if !result.Allowed {
		t.Errorf("go should be allowed: %s", result.Reason)
	}
	result = c.Check("git push")
	if result.Allowed {
		t.Error("git should be denied (matches deny pattern)")
	}
}
```

- [ ] **Step 4: Update TestChecker_Pipes**

```go
func TestChecker_Pipes(t *testing.T) {
	c := NewChecker([]string{"echo", "grep"}, nil, nil)

	result := c.Check("echo hello | grep hello")
	if !result.Allowed {
		t.Errorf("pipe of allowed commands should pass: %s", result.Reason)
	}

	result = c.Check("echo hello | rm -rf /")
	if result.Allowed {
		t.Error("pipe with disallowed command should fail")
	}
}
```

- [ ] **Step 5: Update TestChecker_CommandSubstitution**

```go
func TestChecker_CommandSubstitution(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil, nil)

	result := c.Check("echo $(rm -rf /)")
	if result.Allowed {
		t.Error("command substitution with disallowed command should fail")
	}
}
```

- [ ] **Step 6: Update TestChecker_Subshell**

```go
func TestChecker_Subshell(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil, nil)

	result := c.Check("(rm -rf /)")
	if result.Allowed {
		t.Error("subshell with disallowed command should fail")
	}
}
```

- [ ] **Step 7: Update TestChecker_MetaCommands**

```go
func TestChecker_MetaCommands(t *testing.T) {
	// Even with no allow/deny lists, meta-commands are always blocked
	c := NewChecker(nil, nil, nil)

	tests := []struct {
		cmd     string
		wantErr bool
		errMsg  string
	}{
		{"eval echo hello", true, "eval"},
		{"exec /bin/sh", true, "exec"},
		{"bash -c 'rm -rf /'", true, "bash"},
		{"sh -c 'rm -rf /'", true, "sh"},
		// bash without -c is fine (if not in deny list)
		{"bash --version", false, ""},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed == tt.wantErr {
			t.Errorf("Check(%q).Allowed = %v, wantErr %v", tt.cmd, result.Allowed, tt.wantErr)
		}
		if tt.wantErr && !result.Allowed && !strings.Contains(result.Reason, tt.errMsg) {
			t.Errorf("Check(%q) reason %q should mention %q", tt.cmd, result.Reason, tt.errMsg)
		}
	}
}
```

- [ ] **Step 8: Update TestChecker_VariableExpansion**

```go
func TestChecker_VariableExpansion(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil, nil)

	result := c.Check("$CMD arg1 arg2")
	if result.Allowed {
		t.Error("variable expansion in command name should be blocked")
	}
	result = c.Check("${CMD} arg1")
	if result.Allowed {
		t.Error("braced variable expansion in command name should be blocked")
	}
}
```

- [ ] **Step 9: Update TestChecker_MalformedInput**

```go
func TestChecker_MalformedInput(t *testing.T) {
	c := NewChecker(nil, nil, nil)

	result := c.Check("if then else fi ;; {{")
	if result.Allowed {
		t.Error("malformed input should be rejected")
	}
}
```

- [ ] **Step 10: Update TestChecker_NoRestrictions**

```go
func TestChecker_NoRestrictions(t *testing.T) {
	// No allow or deny = only meta-commands blocked
	c := NewChecker(nil, nil, nil)

	result := c.Check("rm -rf / && curl evil.com")
	if !result.Allowed {
		t.Errorf("no restrictions should allow anything except meta-commands: %s", result.Reason)
	}
}
```

- [ ] **Step 11: Update TestChecker_ChainedCommands**

```go
func TestChecker_ChainedCommands(t *testing.T) {
	c := NewChecker([]string{"go", "echo"}, nil, nil)

	result := c.Check("go build && echo done")
	if !result.Allowed {
		t.Errorf("chained allowed commands should pass: %s", result.Reason)
	}
	result = c.Check("go build && rm -rf /")
	if result.Allowed {
		t.Error("chain with disallowed command should fail")
	}
}
```

- [ ] **Step 12: Update TestChecker_Semicolons**

```go
func TestChecker_Semicolons(t *testing.T) {
	c := NewChecker(nil, []string{"rm *"}, nil)

	result := c.Check("echo hello; rm -rf /")
	if result.Allowed {
		t.Error("semicolon-separated denied command should fail")
	}
}
```

- [ ] **Step 13: Run all permission tests**

Run: `go test -v ./internal/permission`
Expected: All tests PASS

- [ ] **Step 14: Commit**

```bash
git add internal/permission/permission_test.go
git commit -m "test(permission): update existing tests for CheckResult API

All tests now use result.Allowed instead of err != nil.
NewChecker now takes three arguments (allow, deny, autoApprove).

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

### Task 4: Add tests for pipeline auto-approve and deny precedence

**Files:**
- Modify: `internal/permission/permission_test.go` (add new tests)

- [ ] **Step 1: Write test for pipeline auto-approve (all must match)**

Add to `internal/permission/permission_test.go`:

```go
func TestChecker_AutoApprovePipeline(t *testing.T) {
	c := NewChecker(nil, nil, []string{"ls *", "grep *", "wc *"})

	// All commands match autoApprove
	result := c.Check("ls -la | grep foo | wc -l")
	if !result.Allowed || !result.AutoApprove {
		t.Errorf("pipeline of auto-approved commands should auto-approve: Allowed=%v, AutoApprove=%v", result.Allowed, result.AutoApprove)
	}

	// One command doesn't match
	result = c.Check("ls -la | grep foo | sort")
	if !result.Allowed {
		t.Error("pipeline should be allowed (no deny)")
	}
	if result.AutoApprove {
		t.Error("pipeline with non-matching command should NOT auto-approve")
	}
}
```

- [ ] **Step 2: Write test for deny takes precedence over autoApprove**

```go
func TestChecker_DenyPrecedence(t *testing.T) {
	// autoApprove includes rm, but deny blocks it
	c := NewChecker(nil, []string{"rm *"}, []string{"ls *", "rm *"})

	result := c.Check("ls -la")
	if !result.Allowed || !result.AutoApprove {
		t.Errorf("ls should be auto-approved: Allowed=%v, AutoApprove=%v", result.Allowed, result.AutoApprove)
	}

	result = c.Check("rm -rf /")
	if result.Allowed {
		t.Error("rm should be denied even though it matches autoApprove")
	}
}
```

- [ ] **Step 3: Write test for autoApprove implies allow**

```go
func TestChecker_AutoApproveImpliesAllow(t *testing.T) {
	// allow list set, but autoApprove should bypass it
	c := NewChecker([]string{"go"}, nil, []string{"ls *"})

	result := c.Check("go build")
	if !result.Allowed {
		t.Error("go should be allowed (in allow list)")
	}

	result = c.Check("ls -la")
	if !result.Allowed {
		t.Error("ls should be allowed (autoApprove implies allow)")
	}
	if !result.AutoApprove {
		t.Error("ls should be auto-approved")
	}

	result = c.Check("git status")
	if result.Allowed {
		t.Error("git should NOT be allowed (not in allow list, not in autoApprove)")
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test -v ./internal/permission`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/permission/permission_test.go
git commit -m "test(permission): add tests for pipeline auto-approve and deny precedence

- Pipeline auto-approve requires ALL commands to match
- Deny patterns take precedence over autoApprove
- autoApprove patterns imply allow (bypass allow list)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Chunk 2: Config and CLI Changes

### Task 5: Add AutoApprove field to ToolConfig

**Files:**
- Modify: `internal/config/config.go:25-28`
- Modify: `internal/config/config_test.go` (add test)

- [ ] **Step 1: Write failing test for autoApprove config parsing**

Add to `internal/config/config_test.go`:

```go
func TestLoadAutoApprove(t *testing.T) {
	dir := t.TempDir()
	configJSON := `{
		"tools": {
			"bash": {
				"autoApprove": ["ls *", "pwd", "cat *"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "nanocode.json"), []byte(configJSON), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	bashCfg, ok := cfg.Tools["bash"]
	if !ok {
		t.Fatal("expected bash tool config")
	}
	if len(bashCfg.AutoApprove) != 3 {
		t.Errorf("expected 3 autoApprove patterns, got %d", len(bashCfg.AutoApprove))
	}
	if bashCfg.AutoApprove[0] != "ls *" {
		t.Errorf("expected first pattern 'ls *', got %q", bashCfg.AutoApprove[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/config -run TestLoadAutoApprove`
Expected: FAIL (AutoApprove field doesn't exist)

- [ ] **Step 3: Add AutoApprove field to ToolConfig**

Update `internal/config/config.go`:

```go
type ToolConfig struct {
	Allow       []string `json:"allow"`
	Deny        []string `json:"deny"`
	AutoApprove []string `json:"autoApprove"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/config -run TestLoadAutoApprove`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add AutoApprove field to ToolConfig

Allows configuring glob patterns for commands that skip confirmation.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

### Task 6: Add StrictMode to Config and --strict flag

**Files:**
- Modify: `internal/config/config.go:12-23`
- Modify: `main.go:33,149-175`

- [ ] **Step 1: Add StrictMode field to Config**

Update `internal/config/config.go` Config struct:

```go
type Config struct {
	Provider   string                     `json:"provider"`
	Model      string                     `json:"model"`
	APIKey     string                     `json:"apiKey"`
	MaxTokens  int                        `json:"maxTokens"`
	System     string                     `json:"system"`
	Tools      map[string]ToolConfig      `json:"tools"`
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
	BaseURL    string                     `json:"baseURL"`
	ProjectDir string                     `json:"-"` // set by Load(), not from JSON
	StrictMode bool                       `json:"-"` // CLI-only, disables auto-approval
}
```

- [ ] **Step 2: Update parseArgs to return strictMode**

Update `main.go` parseArgs function:

```go
func parseArgs(args []string) (prompt, sessionID string, listMode, strictMode bool, modelOverride string) {
	var parts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				sessionID = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --session requires a value")
			}
		case "--list":
			listMode = true
		case "--strict":
			strictMode = true
		case "--model":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				modelOverride = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "warning: --model requires a value")
			}
		default:
			parts = append(parts, args[i])
		}
	}
	prompt = strings.Join(parts, " ")
	return
}
```

- [ ] **Step 3: Update run() to use strictMode**

Update the run() function in `main.go` line 33:

```go
prompt, sessionID, listMode, strictMode, modelOverride := parseArgs(os.Args[1:])
```

And after config loading (around line 42):

```go
if strictMode {
	cfg.StrictMode = true
}
```

- [ ] **Step 4: Run go build to verify compilation**

Run: `go build -o /dev/null .`
Expected: Success (no errors)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go main.go
git commit -m "feat(cli): add --strict flag to disable auto-approval

When --strict is passed, all commands require Y/n confirmation
regardless of autoApprove patterns in config.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Chunk 3: Engine Integration

### Task 7: Wire auto-approval into engine confirm hook

**Files:**
- Modify: `internal/engine/engine.go:53-66`

- [ ] **Step 1: Update engine New() function**

Replace the permission wiring block in `internal/engine/engine.go`:

```go
	// Permission system: wire allow/deny/autoApprove into bash confirm hook
	if bashCfg, ok := cfg.Tools["bash"]; ok {
		hasPermConfig := len(bashCfg.Allow) > 0 || len(bashCfg.Deny) > 0 || len(bashCfg.AutoApprove) > 0
		if hasPermConfig {
			checker := permission.NewChecker(bashCfg.Allow, bashCfg.Deny, bashCfg.AutoApprove)
			origConfirm := bashTool.ConfirmFunc
			bashTool.ConfirmFunc = func(cmd string) bool {
				result := checker.Check(cmd)
				if !result.Allowed {
					fmt.Fprintf(os.Stderr, "\033[31mBlocked:\033[0m %s\n", result.Reason)
					return false
				}
				if result.AutoApprove && !cfg.StrictMode {
					fmt.Fprintf(os.Stderr, "\033[32mAuto-approved:\033[0m %s\n", cmd)
					return true
				}
				return origConfirm(cmd)
			}
		}
	}
```

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/engine.go
git commit -m "feat(engine): wire auto-approval into bash confirm hook

- Show green 'Auto-approved:' message for auto-approved commands
- Respect --strict flag to disable auto-approval
- Create checker when any of allow/deny/autoApprove is configured

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

### Task 8: Final integration test and cleanup

**Files:**
- All files

- [ ] **Step 1: Run full test suite**

Run: `go test -v ./...`
Expected: All tests PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Run go build**

Run: `go build -o nanocode .`
Expected: Binary builds successfully

- [ ] **Step 4: Manual smoke test**

Create a test directory with config:
```bash
mkdir -p /tmp/nanocode-test
cat > /tmp/nanocode-test/nanocode.json << 'EOF'
{
  "tools": {
    "bash": {
      "autoApprove": ["ls *", "pwd"],
      "deny": ["rm *"]
    }
  }
}
EOF
```

Test from the test directory (requires API key, skip if not available):
```bash
cd /tmp/nanocode-test

# Test auto-approval (should show green "Auto-approved:" and run)
/path/to/nanocode "run ls -la"

# Test deny (should show red "Blocked:")
/path/to/nanocode "run rm -rf /tmp/test"

# Test normal confirmation (should show yellow Y/n prompt)
/path/to/nanocode "run git status"

# Test --strict flag (should show Y/n prompt even for auto-approve pattern)
/path/to/nanocode --strict "run ls -la"
```

- [ ] **Step 5: Verify no new dependencies**

Run: `git diff HEAD go.mod`
Expected: No changes (confirms no new dependencies added per CLAUDE.md rules)

- [ ] **Step 6: Clean up test binary and directory**

Run: `rm -f nanocode && rm -rf /tmp/nanocode-test`

- [ ] **Step 7: Final commit (if any changes needed)**

If any fixes were needed, commit them. Otherwise, skip this step.

---

## Summary

**Total Tasks:** 8
**Estimated Commits:** 8

**Key Files Changed:**
- `internal/permission/permission.go` — matchGlob, CheckResult, updated Check
- `internal/permission/permission_test.go` — all tests updated + new tests
- `internal/config/config.go` — AutoApprove field, StrictMode field
- `internal/config/config_test.go` — new test
- `internal/engine/engine.go` — updated permission wiring
- `main.go` — --strict flag parsing
