package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nanocode/nanocode/internal/provider"
	"github.com/nanocode/nanocode/internal/tool"
)

type mockTool struct {
	name   string
	result string
	err    error
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Definition() provider.ToolDef {
	return provider.ToolDef{Name: m.name, Description: "mock " + m.name, InputSchema: json.RawMessage(`{}`)}
}
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return m.result, m.err
}

// Verify mockTool implements tool.Tool
var _ tool.Tool = (*mockTool)(nil)

func TestRegistryGetAndDefinitions(t *testing.T) {
	r := NewToolRegistry(
		&mockTool{name: "a", result: "1"},
		&mockTool{name: "b", result: "2"},
		&mockTool{name: "c", result: "3"},
	)

	// Get known tool
	tt, ok := r.Get("b")
	if !ok {
		t.Fatal("expected to find tool 'b'")
	}
	if tt.Name() != "b" {
		t.Errorf("expected 'b', got %q", tt.Name())
	}

	// Get unknown tool
	_, ok = r.Get("unknown")
	if ok {
		t.Fatal("expected not to find 'unknown'")
	}

	// Definitions in order
	defs := r.Definitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 defs, got %d", len(defs))
	}
	if defs[0].Name != "a" || defs[1].Name != "b" || defs[2].Name != "c" {
		t.Errorf("unexpected order: %v", defs)
	}
}

func TestRegistryExecuteSuccess(t *testing.T) {
	r := NewToolRegistry(&mockTool{name: "test", result: "ok"})
	result := r.Execute(context.Background(), &provider.ToolCall{
		ID: "tc1", Name: "test", Input: json.RawMessage(`{}`),
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if result.Content != "ok" {
		t.Errorf("expected 'ok', got %q", result.Content)
	}
	if result.ToolCallID != "tc1" {
		t.Errorf("expected tool call ID 'tc1', got %q", result.ToolCallID)
	}
}

func TestRegistryExecuteUnknownTool(t *testing.T) {
	r := NewToolRegistry()
	result := r.Execute(context.Background(), &provider.ToolCall{
		ID: "tc1", Name: "missing", Input: json.RawMessage(`{}`),
	})
	if !result.IsError {
		t.Fatal("expected error for unknown tool")
	}
	if result.Content != "Unknown tool: missing" {
		t.Errorf("unexpected message: %q", result.Content)
	}
}

func TestRegistryExecuteToolError(t *testing.T) {
	r := NewToolRegistry(&mockTool{name: "fail", err: fmt.Errorf("broken")})
	result := r.Execute(context.Background(), &provider.ToolCall{
		ID: "tc1", Name: "fail", Input: json.RawMessage(`{}`),
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
	if result.Content != "Tool error: broken" {
		t.Errorf("unexpected message: %q", result.Content)
	}
}
