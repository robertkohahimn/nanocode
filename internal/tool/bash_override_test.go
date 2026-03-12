package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// --- BashTool confirmation override tests ---

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
