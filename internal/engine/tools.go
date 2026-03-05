package engine

import (
	"context"
	"fmt"

	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/tool"
)

// ToolRegistry manages the set of available tools.
type ToolRegistry struct {
	tools map[string]tool.Tool
	order []string // preserve registration order for definitions
}

// NewToolRegistry creates a registry with the given tools.
func NewToolRegistry(tools ...tool.Tool) *ToolRegistry {
	r := &ToolRegistry{
		tools: make(map[string]tool.Tool, len(tools)),
	}
	for _, t := range tools {
		name := t.Name()
		if _, exists := r.tools[name]; exists {
			continue // skip duplicates
		}
		r.tools[name] = t
		r.order = append(r.order, name)
	}
	return r
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (tool.Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Definitions returns all tool definitions in registration order.
func (r *ToolRegistry) Definitions() []provider.ToolDef {
	defs := make([]provider.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name].Definition())
	}
	return defs
}

// Execute dispatches a tool call and returns the result.
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
