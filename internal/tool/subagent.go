package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type contextKey string

const depthKey contextKey = "nanocode_subagent_depth"
const maxDepth = 3

// EngineRunner is the interface the subagent tool uses to run sub-conversations.
// This avoids a circular import between tool and engine packages.
type EngineRunner interface {
	RunSubagent(ctx context.Context, systemPrompt, task string, onEvent func(provider.Event)) error
}

type SubagentTool struct {
	Runner EngineRunner
}

type subagentInput struct {
	Task    string `json:"task"`
	Context string `json:"context"`
}

func (t *SubagentTool) Name() string { return "subagent" }

func (t *SubagentTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "subagent",
		Description: "Delegate a task to a sub-agent. The sub-agent runs its own conversation loop with access to all tools. Use for independent sub-tasks.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"task": {"type": "string", "description": "Description of the task for the sub-agent"},
				"context": {"type": "string", "description": "Additional context to provide"}
			},
			"required": ["task"]
		}`),
	}
}

func (t *SubagentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if t.Runner == nil {
		return "", fmt.Errorf("sub-agent tool not configured: nil Runner")
	}

	in, err := ParseInput[subagentInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	// Check recursion depth
	depth := GetDepth(ctx)
	if depth >= maxDepth {
		return "", fmt.Errorf("maximum sub-agent depth (%d) reached", maxDepth)
	}

	// Increment depth for sub-context
	subCtx := context.WithValue(ctx, depthKey, depth+1)

	// Build sub-agent system prompt
	systemPrompt := "You are a sub-agent. Complete the following task concisely and return the result. Do not ask clarifying questions."
	if in.Context != "" {
		systemPrompt += "\n\nContext:\n" + in.Context
	}

	// Collect sub-agent output
	var output strings.Builder
	err = t.Runner.RunSubagent(subCtx, systemPrompt, in.Task, func(ev provider.Event) {
		if ev.Type == provider.EventTextDelta {
			output.WriteString(ev.Text)
		}
	})
	if err != nil {
		return "", fmt.Errorf("sub-agent failed: %w", err)
	}

	return TruncateOutput(output.String(), MaxOutputLen), nil
}

// GetDepth returns the current sub-agent recursion depth from context.
func GetDepth(ctx context.Context) int {
	if v, ok := ctx.Value(depthKey).(int); ok {
		return v
	}
	return 0
}
