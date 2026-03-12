# Batch Confirmation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to approve multiple shell commands as a batch instead of one-by-one.

**Architecture:** Add `batch_confirm.go` for selection parsing and batch prompting, enhance `BashTool` with confirmation overrides, integrate batch collection in engine's tool execution loop.

**Tech Stack:** Go, bufio for stdin, standard library only

---

## Chunk 1: Selection Parsing and Data Structures

### Task 1: Create batch_confirm.go with data structures

**Files:**
- Create: `internal/engine/batch_confirm.go`
- Test: `internal/engine/batch_confirm_test.go`

- [ ] **Step 1: Write the failing test for parseSelection with "Y" input**

```go
// internal/engine/batch_confirm_test.go
package engine

import (
	"testing"
)

func TestParseSelection_All(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"Y", 3},
		{"y", 3},
		{"", 3},
		{"yes", 3},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSelection(tt.input, tt.count)
			if err != nil {
				t.Fatalf("parseSelection(%q, %d): %v", tt.input, tt.count, err)
			}
			if len(result) != tt.count {
				t.Errorf("expected %d selections, got %d", tt.count, len(result))
			}
			for i := 0; i < tt.count; i++ {
				if !result[i] {
					t.Errorf("expected index %d to be selected", i)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestParseSelection_All -v`
Expected: FAIL - parseSelection undefined

- [ ] **Step 3: Write minimal implementation for parseSelection (approve all case)**

```go
// internal/engine/batch_confirm.go
package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// parseSelection parses user input and returns a slice of booleans indicating
// which indices (0-based) are selected. count is the total number of commands.
func parseSelection(input string, count int) ([]bool, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	result := make([]bool, count)

	// Empty, "y", or "yes" = approve all
	if input == "" || input == "y" || input == "yes" {
		for i := range result {
			result[i] = true
		}
		return result, nil
	}

	return result, fmt.Errorf("unrecognized input: %q", input)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestParseSelection_All -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/batch_confirm.go internal/engine/batch_confirm_test.go
git commit -m "feat(batch): add parseSelection with approve-all case"
```

---

### Task 2: Add parseSelection reject-all case

**Files:**
- Modify: `internal/engine/batch_confirm.go`
- Modify: `internal/engine/batch_confirm_test.go`

- [ ] **Step 1: Write the failing test for "n" input**

```go
// Add to batch_confirm_test.go
func TestParseSelection_None(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"N"},
		{"n"},
		{"no"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSelection(tt.input, 3)
			if err != nil {
				t.Fatalf("parseSelection(%q, 3): %v", tt.input, err)
			}
			for i, selected := range result {
				if selected {
					t.Errorf("expected index %d to NOT be selected", i)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestParseSelection_None -v`
Expected: FAIL - unrecognized input

- [ ] **Step 3: Add reject-all case to parseSelection**

```go
// In parseSelection, after approve-all check, add:
	// "n" or "no" = reject all
	if input == "n" || input == "no" {
		return result, nil // all false
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestParseSelection_None -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/batch_confirm.go internal/engine/batch_confirm_test.go
git commit -m "feat(batch): add parseSelection reject-all case"
```

---

### Task 3: Add parseSelection number selection

**Files:**
- Modify: `internal/engine/batch_confirm.go`
- Modify: `internal/engine/batch_confirm_test.go`

- [ ] **Step 1: Write the failing test for comma-separated numbers**

```go
// Add to batch_confirm_test.go
func TestParseSelection_Numbers(t *testing.T) {
	tests := []struct {
		input    string
		count    int
		expected []bool
	}{
		{"1", 3, []bool{true, false, false}},
		{"1,3", 3, []bool{true, false, true}},
		{"2,3", 3, []bool{false, true, true}},
		{"1,2,3", 3, []bool{true, true, true}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSelection(tt.input, tt.count)
			if err != nil {
				t.Fatalf("parseSelection(%q, %d): %v", tt.input, tt.count, err)
			}
			for i, want := range tt.expected {
				if result[i] != want {
					t.Errorf("index %d: got %v, want %v", i, result[i], want)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestParseSelection_Numbers -v`
Expected: FAIL

- [ ] **Step 3: Add number parsing to parseSelection**

```go
// In parseSelection, after reject-all check, replace the error return with:
	// Parse comma/space-separated numbers
	// Replace commas with spaces for uniform splitting
	input = strings.ReplaceAll(input, ",", " ")
	parts := strings.Fields(input)

	for _, part := range parts {
		// Check for range (e.g., "1-3")
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range: %q", part)
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("invalid range: %q", part)
			}
			if start < 1 || end > count || start > end {
				return nil, fmt.Errorf("invalid range %d-%d for %d commands", start, end, count)
			}
			for i := start; i <= end; i++ {
				result[i-1] = true
			}
			continue
		}

		// Single number
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid selection: %q", part)
		}
		if n < 1 || n > count {
			return nil, fmt.Errorf("selection %d out of range (1-%d)", n, count)
		}
		result[n-1] = true
	}

	return result, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestParseSelection_Numbers -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/batch_confirm.go internal/engine/batch_confirm_test.go
git commit -m "feat(batch): add parseSelection number and range parsing"
```

---

### Task 4: Add parseSelection space-separated and range tests

**Files:**
- Modify: `internal/engine/batch_confirm_test.go`

- [ ] **Step 1: Write tests for space-separated and range syntax**

```go
// Add to batch_confirm_test.go
func TestParseSelection_Spaces(t *testing.T) {
	result, err := parseSelection("1 3", 4)
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	expected := []bool{true, false, true, false}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("index %d: got %v, want %v", i, result[i], want)
		}
	}
}

func TestParseSelection_Range(t *testing.T) {
	result, err := parseSelection("1-3", 4)
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	expected := []bool{true, true, true, false}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("index %d: got %v, want %v", i, result[i], want)
		}
	}
}

func TestParseSelection_Invalid(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"abc", 3},
		{"0", 3},      // out of range (1-based)
		{"4", 3},      // out of range
		{"1-5", 3},    // range exceeds count
		{"3-1", 3},    // inverted range
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseSelection(tt.input, tt.count)
			if err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/engine/... -run "TestParseSelection_(Spaces|Range|Invalid)" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/batch_confirm_test.go
git commit -m "test(batch): add space-separated, range, and invalid input tests"
```

---

## Chunk 2: Batch Prompt UI

### Task 5: Add batchDecision type and promptBatch function

**Files:**
- Modify: `internal/engine/batch_confirm.go`
- Modify: `internal/engine/batch_confirm_test.go`

- [ ] **Step 1: Write the failing test for promptBatch**

```go
// Add to batch_confirm_test.go
import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestPromptBatch_ApproveAll(t *testing.T) {
	// Simulate user typing "Y\n"
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)

	var output bytes.Buffer

	commands := []pendingCommand{
		{toolCallID: "tc1", command: "mkdir -p project"},
		{toolCallID: "tc2", command: "pwd"},
		{toolCallID: "tc3", command: "ls"},
	}

	decisions, err := promptBatch(commands, reader, &output)
	if err != nil {
		t.Fatalf("promptBatch: %v", err)
	}

	// All should be approved
	for _, cmd := range commands {
		dec, ok := decisions[cmd.toolCallID]
		if !ok {
			t.Errorf("missing decision for %s", cmd.toolCallID)
			continue
		}
		if !dec.approved {
			t.Errorf("expected %s to be approved", cmd.toolCallID)
		}
		if dec.skipped {
			t.Errorf("expected %s to not be skipped", cmd.toolCallID)
		}
	}

	// Check output contains the commands
	outStr := output.String()
	if !strings.Contains(outStr, "mkdir -p project") {
		t.Errorf("output should contain command, got: %s", outStr)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestPromptBatch_ApproveAll -v`
Expected: FAIL - pendingCommand and promptBatch undefined

- [ ] **Step 3: Add batchDecision, pendingCommand types and promptBatch function**

```go
// Add to batch_confirm.go
import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// batchDecision represents the user's decision for a single command.
type batchDecision struct {
	approved bool
	skipped  bool // true if user selected others but not this one
}

// pendingCommand represents a bash command awaiting confirmation.
type pendingCommand struct {
	toolCallID string
	command    string
}

// promptBatch displays a numbered list of commands and prompts for selection.
// Returns a map of toolCallID -> decision.
func promptBatch(commands []pendingCommand, reader *bufio.Reader, output io.Writer) (map[string]batchDecision, error) {
	// Display numbered list
	fmt.Fprintf(output, "\033[33mPending commands:\033[0m\n")
	for i, cmd := range commands {
		fmt.Fprintf(output, "  %d. %s\n", i+1, cmd.command)
	}
	fmt.Fprintf(output, "\nRun all? \033[2m[Y/n/1,3,4]\033[0m ")

	// Read user input
	line, err := reader.ReadString('\n')
	if err != nil {
		// On EOF or error, reject all
		result := make(map[string]batchDecision)
		for _, cmd := range commands {
			result[cmd.toolCallID] = batchDecision{approved: false, skipped: false}
		}
		return result, nil
	}

	// Parse selection
	selected, err := parseSelection(line, len(commands))
	if err != nil {
		// On parse error, ask again (for now, just return the error)
		return nil, fmt.Errorf("invalid selection: %w", err)
	}

	// Build decisions map
	result := make(map[string]batchDecision)
	anySelected := false
	for _, s := range selected {
		if s {
			anySelected = true
			break
		}
	}

	for i, cmd := range commands {
		if selected[i] {
			result[cmd.toolCallID] = batchDecision{approved: true, skipped: false}
		} else {
			// skipped = true only if user selected others (partial selection)
			result[cmd.toolCallID] = batchDecision{approved: false, skipped: anySelected}
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestPromptBatch_ApproveAll -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/batch_confirm.go internal/engine/batch_confirm_test.go
git commit -m "feat(batch): add promptBatch function with batch UI"
```

---

### Task 6: Add tests for partial selection and reject all

**Files:**
- Modify: `internal/engine/batch_confirm_test.go`

- [ ] **Step 1: Write tests for partial selection and reject all**

```go
// Add to batch_confirm_test.go
func TestPromptBatch_PartialSelection(t *testing.T) {
	input := bytes.NewBufferString("1,3\n")
	reader := bufio.NewReader(input)
	var output bytes.Buffer

	commands := []pendingCommand{
		{toolCallID: "tc1", command: "mkdir"},
		{toolCallID: "tc2", command: "pwd"},
		{toolCallID: "tc3", command: "ls"},
	}

	decisions, err := promptBatch(commands, reader, &output)
	if err != nil {
		t.Fatalf("promptBatch: %v", err)
	}

	// tc1 and tc3 approved, tc2 skipped
	if !decisions["tc1"].approved {
		t.Error("tc1 should be approved")
	}
	if decisions["tc2"].approved {
		t.Error("tc2 should NOT be approved")
	}
	if !decisions["tc2"].skipped {
		t.Error("tc2 should be marked as skipped")
	}
	if !decisions["tc3"].approved {
		t.Error("tc3 should be approved")
	}
}

func TestPromptBatch_RejectAll(t *testing.T) {
	input := bytes.NewBufferString("n\n")
	reader := bufio.NewReader(input)
	var output bytes.Buffer

	commands := []pendingCommand{
		{toolCallID: "tc1", command: "mkdir"},
		{toolCallID: "tc2", command: "pwd"},
	}

	decisions, err := promptBatch(commands, reader, &output)
	if err != nil {
		t.Fatalf("promptBatch: %v", err)
	}

	for _, cmd := range commands {
		dec := decisions[cmd.toolCallID]
		if dec.approved {
			t.Errorf("%s should NOT be approved", cmd.toolCallID)
		}
		if dec.skipped {
			t.Errorf("%s should NOT be skipped (reject all, not partial)", cmd.toolCallID)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/engine/... -run "TestPromptBatch_(PartialSelection|RejectAll)" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/batch_confirm_test.go
git commit -m "test(batch): add partial selection and reject all tests"
```

---

## Chunk 3: BashTool Override Support

### Task 7: Add confirmation override data structures to BashTool

**Files:**
- Modify: `internal/tool/bash.go`
- Modify: `internal/tool/tool_test.go`

**Important:** This task also exports `bashInput` as `BashInput` for use by the engine package.

- [ ] **Step 1: Write the failing test for confirmation override**

```go
// Add to tool_test.go

// mockToolCallIDContext creates a context with a mock tool call ID getter
func mockToolCallIDContext(id string) context.Context {
	return context.WithValue(context.Background(), "tool_call_id", id)
}

func TestBashTool_ConfirmOverride_Approved(t *testing.T) {
	bt := NewBashTool(nil)
	// Mock the tool call ID getter to return our test ID
	bt.SetToolCallIDGetter(func(ctx context.Context) string {
		if id, ok := ctx.Value("tool_call_id").(string); ok {
			return id
		}
		return ""
	})
	// Set an override that approves without prompting
	bt.SetConfirmOverride("test-id", true, false)
	defer bt.ClearConfirmOverrides()

	// Execute should succeed without calling ConfirmFunc
	confirmCalled := false
	bt.ConfirmFunc = func(string) bool {
		confirmCalled = true
		return false // would reject if called
	}

	ctx := mockToolCallIDContext("test-id")
	result, err := bt.Execute(ctx, json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalled {
		t.Error("ConfirmFunc should not have been called")
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected hello, got %q", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/... -run TestBashTool_ConfirmOverride_Approved -v`
Expected: FAIL - SetConfirmOverride, SetToolCallIDGetter undefined

- [ ] **Step 3: Add override fields, methods, and export BashInput**

```go
// Modify internal/tool/bash.go

// FIRST: Export bashInput by renaming to BashInput (line 23-26)
// Change from:
//   type bashInput struct {
// To:
type BashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // seconds
}

// Update BashTool struct with all fields:
type BashTool struct {
	ConfirmFunc      func(command string) bool
	stdinReader      *bufio.Reader
	confirmOverrides map[string]bashOverride        // NEW
	getToolCallID    func(ctx context.Context) string // NEW
}

// NEW: bashOverride stores the pre-determined decision for a tool call
type bashOverride struct {
	approved bool
	skipped  bool
}

// NEW: SetConfirmOverride sets a pre-determined decision for a tool call ID
func (t *BashTool) SetConfirmOverride(toolCallID string, approved, skipped bool) {
	if t.confirmOverrides == nil {
		t.confirmOverrides = make(map[string]bashOverride)
	}
	t.confirmOverrides[toolCallID] = bashOverride{approved: approved, skipped: skipped}
}

// NEW: ClearConfirmOverrides removes all overrides
func (t *BashTool) ClearConfirmOverrides() {
	t.confirmOverrides = nil
}

// NEW: SetToolCallIDGetter sets the function to retrieve tool call ID from context
func (t *BashTool) SetToolCallIDGetter(fn func(ctx context.Context) string) {
	t.getToolCallID = fn
}

// NEW: extract command execution to reuse
func (t *BashTool) executeCommand(ctx context.Context, in BashInput) (string, error) {
	timeout := 30
	if in.Timeout > 0 {
		timeout = in.Timeout
	}
	if timeout > 300 {
		timeout = 300
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", in.Command)
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	cmd.Dir = wd

	output, err := cmd.CombinedOutput()
	result := string(output)

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			result += fmt.Sprintf("\n(timed out after %ds)", timeout)
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		result = fmt.Sprintf("Exit code %d\n%s", exitCode, result)
	}

	return TruncateOutput(result, MaxOutputLen), nil
}

// Update Execute to check overrides first:
func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[BashInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	// Check for override using tool call ID from context
	if t.confirmOverrides != nil && t.getToolCallID != nil {
		if toolCallID := t.getToolCallID(ctx); toolCallID != "" {
			if override, ok := t.confirmOverrides[toolCallID]; ok {
				if override.skipped {
					return "Command skipped (user selected others from batch)", nil
				}
				if !override.approved {
					return "Command rejected by user", nil
				}
				// approved: skip confirmation, proceed to execution
				return t.executeCommand(ctx, in)
			}
		}
	}

	// No override: use normal confirmation
	confirm := t.ConfirmFunc
	if confirm == nil {
		confirm = t.defaultConfirm
	}
	if !confirm(in.Command) {
		return "Command rejected by user", nil
	}

	return t.executeCommand(ctx, in)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/... -run TestBashTool_ConfirmOverride_Approved -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tool/bash.go internal/tool/tool_test.go
git commit -m "feat(bash): add confirmation override support"
```

---

### Task 8: Add tests for skipped and fallback behavior

**Files:**
- Modify: `internal/tool/tool_test.go`

- [ ] **Step 1: Write tests for skipped override and no-override fallback**

```go
// Add to tool_test.go
func TestBashTool_ConfirmOverride_Skipped(t *testing.T) {
	bt := NewBashTool(nil)
	bt.SetToolCallIDGetter(func(ctx context.Context) string {
		if id, ok := ctx.Value("tool_call_id").(string); ok {
			return id
		}
		return ""
	})
	bt.SetConfirmOverride("test-id", false, true) // skipped
	defer bt.ClearConfirmOverrides()

	ctx := mockToolCallIDContext("test-id")
	result, err := bt.Execute(ctx, json.RawMessage(`{"command":"echo nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "skipped") {
		t.Errorf("expected skipped message, got %q", result)
	}
}

func TestBashTool_NoOverride_FallsBack(t *testing.T) {
	bt := NewBashTool(nil)
	bt.SetToolCallIDGetter(func(ctx context.Context) string {
		if id, ok := ctx.Value("tool_call_id").(string); ok {
			return id
		}
		return ""
	})
	// Set override for different ID
	bt.SetConfirmOverride("other-id", true, false)
	defer bt.ClearConfirmOverrides()

	confirmCalled := false
	bt.ConfirmFunc = func(string) bool {
		confirmCalled = true
		return true
	}

	// Execute with ID that has no override
	ctx := mockToolCallIDContext("test-id")
	result, err := bt.Execute(ctx, json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !confirmCalled {
		t.Error("ConfirmFunc should have been called for non-overridden ID")
	}
	if !strings.Contains(result, "hi") {
		t.Errorf("expected hi, got %q", result)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/tool/... -run "TestBashTool_(ConfirmOverride_Skipped|NoOverride_FallsBack)" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tool/tool_test.go
git commit -m "test(bash): add skipped override and fallback tests"
```

---

## Chunk 4: Engine Integration

### Task 9: Pass tool call ID through context in ToolRegistry

**Files:**
- Modify: `internal/engine/tools.go`
- Modify: `internal/engine/tools_test.go`
- Modify: `internal/tool/bash.go`

The critical fix: `ToolRegistry.Execute()` must pass the tool call ID to individual tools so BashTool can look up overrides. We'll use context to pass the ID.

- [ ] **Step 1: Add context key and getter for tool call ID**

```go
// Add to internal/engine/tools.go

// toolCallIDKey is the context key for the tool call ID.
type toolCallIDKey struct{}

// ToolCallIDFromContext retrieves the tool call ID from context.
func ToolCallIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(toolCallIDKey{}).(string); ok {
		return id
	}
	return ""
}
```

- [ ] **Step 2: Modify ToolRegistry.Execute to set tool call ID in context**

```go
// In internal/engine/tools.go, modify Execute():
func (r *ToolRegistry) Execute(ctx context.Context, tc *provider.ToolCall) *provider.ToolResult {
	if tc == nil {
		return &provider.ToolResult{
			Content: "nil tool call",
			IsError: true,
		}
	}
	t, ok := r.tools[tc.Name]
	if !ok {
		return &provider.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("Unknown tool: %s", tc.Name),
			IsError:    true,
		}
	}

	// Pass tool call ID through context
	ctx = context.WithValue(ctx, toolCallIDKey{}, tc.ID)

	result, err := t.Execute(ctx, tc.Input)
	if err != nil {
		return &provider.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("Tool error: %s", err.Error()),
			IsError:    true,
		}
	}

	return &provider.ToolResult{
		ToolCallID: tc.ID,
		Content:    result,
		IsError:    false,
	}
}
```

- [ ] **Step 3: Update BashTool.Execute to read tool call ID from context**

```go
// In internal/tool/bash.go, update Execute():
func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[BashInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	// Check for override using tool call ID from context
	// Import engine package would create cycle, so we use a callback instead
	if t.confirmOverrides != nil {
		if toolCallID := t.getToolCallID(ctx); toolCallID != "" {
			if override, ok := t.confirmOverrides[toolCallID]; ok {
				if override.skipped {
					return "Command skipped (user selected others from batch)", nil
				}
				if !override.approved {
					return "Command rejected by user", nil
				}
				// approved: skip confirmation, proceed to execution
				return t.executeCommand(ctx, in)
			}
		}
	}

	// No override: use normal confirmation
	confirm := t.ConfirmFunc
	if confirm == nil {
		confirm = t.defaultConfirm
	}
	if !confirm(in.Command) {
		return "Command rejected by user", nil
	}

	return t.executeCommand(ctx, in)
}
```

- [ ] **Step 4: Verify BashTool has getToolCallID field**

Note: The `getToolCallID` field and `SetToolCallIDGetter` method were already added in Task 7 Step 3.
Just verify they exist:

Run: `grep -n "getToolCallID" internal/tool/bash.go`
Expected: Shows the field and setter method

- [ ] **Step 5: Wire up the getter in engine.New()**

```go
// In internal/engine/engine.go, in New() function, immediately after line 51:
// After: bashTool := tool.NewBashTool(stdinReader)
// Add:
bashTool.SetToolCallIDGetter(ToolCallIDFromContext)
```

- [ ] **Step 6: Run tests to verify no regression**

Run: `go test ./internal/engine/... ./internal/tool/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/engine/tools.go internal/tool/bash.go
git commit -m "feat(engine): pass tool call ID through context for override lookup"
```

---

### Task 10: Export BashInput for engine access

**Files:**
- Modify: `internal/tool/bash.go`

Note: BashInput was already exported in Task 7 Step 3 (changed from `bashInput` to `BashInput`). This task verifies the export is complete.

- [ ] **Step 1: Verify BashInput is exported**

Run: `grep "type BashInput" internal/tool/bash.go`
Expected: `type BashInput struct {`

- [ ] **Step 2: Run existing tests to ensure no regression**

Run: `go test ./internal/tool/... -v`
Expected: PASS

- [ ] **Step 3: Commit if changes needed**

```bash
git status internal/tool/bash.go
# Only commit if there are uncommitted changes
```

---

### Task 11: Add collectBashConfirmations to engine

**Files:**
- Modify: `internal/engine/batch_confirm.go`
- Modify: `internal/engine/engine.go`
- Modify: `internal/engine/engine_test.go`

- [ ] **Step 1: Write the failing integration test**

```go
// Add to engine_test.go
func TestEngine_BatchConfirm_MultipleBash(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// Response with multiple bash tool calls
			{
				{Type: provider.EventTextDelta, Text: "Running commands"},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo one"}`),
				}},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc2",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo two"}`),
				}},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc3",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo three"}`),
				}},
				{Type: provider.EventDone},
			},
			// Final response
			{
				{Type: provider.EventTextDelta, Text: "Done"},
				{Type: provider.EventDone},
			},
		},
	}

	// Create engine with mock stdin that approves batch
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)

	eng := New(mp, st, testConfig(), reader)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "run three commands", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have made 2 provider calls
	if mp.callIdx != 2 {
		t.Errorf("expected 2 provider calls, got %d", mp.callIdx)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/... -run TestEngine_BatchConfirm_MultipleBash -v`
Expected: FAIL (or multiple prompts for Y/n)

- [ ] **Step 3: Add collectBashConfirmations function**

```go
// Add to internal/engine/batch_confirm.go
import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/permission"
	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/tool"
)

// collectBashConfirmations identifies bash tool calls needing confirmation,
// prompts for batch approval if multiple, and returns the decisions.
// Returns nil if no batch prompt was shown (0-1 commands needing confirmation).
func collectBashConfirmations(
	toolCalls []*provider.ToolCall,
	checker *permission.Checker,
	reader *bufio.Reader,
	output io.Writer,
) map[string]batchDecision {
	// Collect bash commands that need confirmation
	var pending []pendingCommand
	for _, tc := range toolCalls {
		if tc.Name != "bash" {
			continue
		}
		// Parse command
		in, err := tool.ParseInput[tool.BashInput](tc.Input)
		if err != nil {
			continue // will fail during execution
		}
		// Check permission
		if checker != nil {
			if err := checker.Check(in.Command); err == nil {
				continue // allowed by permission, no confirmation needed
			}
		}
		pending = append(pending, pendingCommand{
			toolCallID: tc.ID,
			command:    in.Command,
		})
	}

	// No batch needed: 0-1 commands
	if len(pending) <= 1 {
		return nil
	}

	// Show batch prompt
	if output == nil {
		output = os.Stderr
	}
	decisions, err := promptBatch(pending, reader, output)
	if err != nil {
		// On error, reject all
		decisions = make(map[string]batchDecision)
		for _, cmd := range pending {
			decisions[cmd.toolCallID] = batchDecision{approved: false, skipped: false}
		}
	}
	return decisions
}
```

- [ ] **Step 4: Add new fields to Engine struct**

```go
// Modify internal/engine/engine.go lines 39-46
// Add three new fields to the Engine struct:
type Engine struct {
	provider    provider.Provider
	tools       *ToolRegistry
	store       store.Store
	config      *config.Config
	mcpClients  []io.Closer
	snapMgr     *snapshot.Manager
	stdinReader *bufio.Reader         // NEW: for batch confirmation
	bashTool    *tool.BashTool        // NEW: direct reference for overrides
	permChecker *permission.Checker   // NEW: for batch filtering
}
```

- [ ] **Step 5: Update New() to store references**

```go
// Modify internal/engine/engine.go New() function
// After line 51 (bashTool := tool.NewBashTool(stdinReader)), the existing permission
// setup already creates a checker. Store it:

// Find the existing checker creation (~line 56) and add after line 66:
// After the closing brace of: if len(bashCfg.Allow) > 0 || len(bashCfg.Deny) > 0 { ... }
// The permChecker variable is already created, just needs to be stored.

// Update the eng := &Engine{...} initialization (~line 143) to include:
	eng := &Engine{
		provider:    p,
		store:       s,
		config:      cfg,
		mcpClients:  mcpClients,
		snapMgr:     snapMgr,
		stdinReader: stdinReader,   // NEW
		bashTool:    bashTool,      // NEW
		permChecker: permChecker,   // NEW (move from local var)
	}
```

- [ ] **Step 6: Add batch confirmation to loop()**

```go
// Modify internal/engine/engine.go loop() function
// Insert AFTER line 316 (closing brace of tool calls extraction):
//   }  // end of: for _, cb := range assistantMsg.Content
//
// And BEFORE line 318 (the "If no tool calls" comment):
//   // If no tool calls, we are done

// Insert this code block between them:

		// Batch confirmation for multiple bash commands (NEW)
		if e.stdinReader != nil {
			overrides := collectBashConfirmations(toolCalls, e.permChecker, e.stdinReader, os.Stderr)
			if overrides != nil {
				for id, dec := range overrides {
					e.bashTool.SetConfirmOverride(id, dec.approved, dec.skipped)
				}
				defer e.bashTool.ClearConfirmOverrides()
			}
		}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestEngine_BatchConfirm_MultipleBash -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/engine/batch_confirm.go internal/engine/engine.go internal/engine/engine_test.go
git commit -m "feat(engine): integrate batch confirmation for multiple bash commands"
```

---

### Task 12: Add test for single bash fallback

**Files:**
- Modify: `internal/engine/engine_test.go`

- [ ] **Step 1: Write test for single bash command (no batch prompt)**

```go
// Add to engine_test.go
func TestEngine_BatchConfirm_SingleBash(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Running"},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo single"}`),
				}},
				{Type: provider.EventDone},
			},
			{
				{Type: provider.EventTextDelta, Text: "Done"},
				{Type: provider.EventDone},
			},
		},
	}

	// Single bash command should use standard Y/n prompt
	// Simulate typing "Y\n" for the standard prompt
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)

	eng := New(mp, st, testConfig(), reader)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "run one command", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mp.callIdx != 2 {
		t.Errorf("expected 2 provider calls, got %d", mp.callIdx)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestEngine_BatchConfirm_SingleBash -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/engine_test.go
git commit -m "test(engine): add single bash command fallback test"
```

---

### Task 13: Add test for mixed tools ordering

**Files:**
- Modify: `internal/engine/engine_test.go`

- [ ] **Step 1: Write test for mixed tool calls preserving order**

```go
// Add to engine_test.go
func TestEngine_BatchConfirm_MixedTools(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Mixed"},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "read",
					Input: json.RawMessage(`{"file_path":"/dev/null"}`),
				}},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc2",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo one"}`),
				}},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc3",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"echo two"}`),
				}},
				{Type: provider.EventDone},
			},
			{
				{Type: provider.EventTextDelta, Text: "Done"},
				{Type: provider.EventDone},
			},
		},
	}

	// Batch prompt for 2 bash commands
	input := bytes.NewBufferString("Y\n")
	reader := bufio.NewReader(input)

	eng := New(mp, st, testConfig(), reader)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "mixed tools", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify tools executed (2 bash + 1 read)
	if mp.callIdx != 2 {
		t.Errorf("expected 2 provider calls, got %d", mp.callIdx)
	}

	// Second request should have 3 tool results
	if len(mp.requests) < 2 {
		t.Fatal("expected 2 requests")
	}
	resultMsg := mp.requests[1].Messages[len(mp.requests[1].Messages)-1]
	toolResults := 0
	for _, cb := range resultMsg.Content {
		if cb.Type == "tool_result" {
			toolResults++
		}
	}
	if toolResults != 3 {
		t.Errorf("expected 3 tool results, got %d", toolResults)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/engine/... -run TestEngine_BatchConfirm_MixedTools -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/engine/engine_test.go
git commit -m "test(engine): add mixed tools ordering test"
```

---

## Chunk 5: Final Integration and Cleanup

### Task 14: Run all tests and verify

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 2: Run go vet and check for issues**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Verify file line counts**

Run: `wc -l internal/engine/batch_confirm.go internal/tool/bash.go`
Expected: Both under 500 lines

- [ ] **Step 4: Final commit for any cleanup**

```bash
git add -A
git commit -m "chore: cleanup and finalize batch confirmation feature" --allow-empty
```

---

### Task 15: Update documentation

**Files:**
- Modify: `docs/superpowers/specs/2026-03-12-batch-confirmation-design.md`

- [ ] **Step 1: Mark spec as implemented**

```markdown
# At top of file, change:
**Status:** Approved
# To:
**Status:** Implemented
```

- [ ] **Step 2: Commit documentation update**

```bash
git add docs/superpowers/specs/2026-03-12-batch-confirmation-design.md
git commit -m "docs: mark batch confirmation spec as implemented"
```

---

## Summary

**Total Tasks:** 15
**New Files:** 2 (`batch_confirm.go`, `batch_confirm_test.go`)
**Modified Files:** 5 (`bash.go`, `tool_test.go`, `tools.go`, `engine.go`, `engine_test.go`)
**Estimated New Code:** ~180 lines
**Estimated Test Code:** ~220 lines
