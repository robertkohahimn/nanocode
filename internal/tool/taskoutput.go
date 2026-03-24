package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// TaskOutputTool retrieves the status and output of a background bash task.
type TaskOutputTool struct {
	Manager *BackgroundTaskManager
}

type taskOutputInput struct {
	TaskID string `json:"task_id"`
}

func (t *TaskOutputTool) Name() string { return "task_output" }

func (t *TaskOutputTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "task_output",
		Description: "Get the status and output of a background bash task.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"task_id": {"type": "string", "description": "The background task ID (e.g. bg_abc123)"}
			},
			"required": ["task_id"]
		}`),
	}
}

func (t *TaskOutputTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[taskOutputInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}
	if in.TaskID == "" {
		return "", fmt.Errorf("task_id is required")
	}

	task := t.Manager.Get(in.TaskID)
	if task == nil {
		return "", fmt.Errorf("background task not found: %s", in.TaskID)
	}

	output, err := t.Manager.ReadOutput(in.TaskID)
	if err != nil {
		return "", err
	}

	task.mu.Lock()
	status := task.Status
	exitCode := task.ExitCode
	task.mu.Unlock()

	if status == "running" {
		return fmt.Sprintf("Status: %s\n\n%s", status, output), nil
	}
	return fmt.Sprintf("Status: %s\nExit code: %d\n\n%s", status, exitCode, output), nil
}
