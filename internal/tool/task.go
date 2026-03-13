package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/store"
)

// --- TaskCreateTool ---

// TaskCreateTool creates a new task in the current session.
type TaskCreateTool struct {
	Store        store.Store
	GetSessionID func() string
}

type taskCreateInput struct {
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

func (t *TaskCreateTool) Name() string { return "task_create" }

func (t *TaskCreateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "task_create",
		Description: "Create a new task to track work. Returns the task ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"subject": {"type": "string", "description": "Short, actionable subject for the task"},
				"description": {"type": "string", "description": "Detailed description of what needs to be done"}
			},
			"required": ["subject"]
		}`),
	}
}

func (t *TaskCreateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[taskCreateInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}
	if in.Subject == "" {
		return "", fmt.Errorf("subject is required")
	}

	sessionID := t.GetSessionID()
	if sessionID == "" {
		return "", fmt.Errorf("no active session")
	}

	id, err := t.Store.CreateTask(ctx, sessionID, in.Subject, in.Description)
	if err != nil {
		return "", fmt.Errorf("creating task: %w", err)
	}

	return fmt.Sprintf("Created task %s: %s", id, in.Subject), nil
}

// --- TaskUpdateTool ---

// TaskUpdateTool updates an existing task's status, subject, or description.
type TaskUpdateTool struct {
	Store        store.Store
	GetSessionID func() string
}

type taskUpdateInput struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	BlockedBy   []string `json:"blocked_by"`
}

func (t *TaskUpdateTool) Name() string { return "task_update" }

func (t *TaskUpdateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "task_update",
		Description: "Update a task's status, subject, description, or dependencies. Use status 'deleted' to remove a task.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id": {"type": "string", "description": "Task ID to update"},
				"status": {"type": "string", "enum": ["pending", "in_progress", "completed", "deleted"], "description": "New status"},
				"subject": {"type": "string", "description": "Updated subject"},
				"description": {"type": "string", "description": "Updated description"},
				"blocked_by": {"type": "array", "items": {"type": "string"}, "description": "Task IDs this task is blocked by"}
			},
			"required": ["id"]
		}`),
	}
}

func (t *TaskUpdateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[taskUpdateInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}
	if in.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	// Handle deletion
	if in.Status == "deleted" {
		if err := t.Store.DeleteTask(ctx, in.ID); err != nil {
			return "", fmt.Errorf("deleting task: %w", err)
		}
		return fmt.Sprintf("Deleted task %s", in.ID), nil
	}

	// Build update
	updates := store.TaskUpdate{}
	if in.Subject != "" {
		updates.Subject = &in.Subject
	}
	if in.Description != "" {
		updates.Description = &in.Description
	}
	if in.Status != "" {
		updates.Status = &in.Status
	}

	if updates.Subject != nil || updates.Description != nil || updates.Status != nil {
		if err := t.Store.UpdateTask(ctx, in.ID, updates); err != nil {
			return "", fmt.Errorf("updating task: %w", err)
		}
	}

	// Add dependencies
	for _, dep := range in.BlockedBy {
		if err := t.Store.AddTaskDep(ctx, in.ID, dep); err != nil {
			return "", fmt.Errorf("adding dependency: %w", err)
		}
	}

	return fmt.Sprintf("Updated task %s", in.ID), nil
}

// --- TaskListTool ---

// TaskListTool lists all tasks for the current session.
type TaskListTool struct {
	Store        store.Store
	GetSessionID func() string
}

func (t *TaskListTool) Name() string { return "task_list" }

func (t *TaskListTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "task_list",
		Description: "List all tasks for the current session with their status.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`),
	}
}

func (t *TaskListTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	sessionID := t.GetSessionID()
	if sessionID == "" {
		return "", fmt.Errorf("no active session")
	}

	tasks, err := t.Store.ListTasks(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("listing tasks: %w", err)
	}

	if len(tasks) == 0 {
		return "No tasks found for this session.", nil
	}

	var buf strings.Builder
	for _, task := range tasks {
		icon := statusIcon(task.Status)
		fmt.Fprintf(&buf, "%s [%s] %s (id: %s)\n", icon, task.Status, task.Subject, task.ID)
		if len(task.BlockedBy) > 0 {
			fmt.Fprintf(&buf, "  blocked by: %s\n", strings.Join(task.BlockedBy, ", "))
		}
	}

	return buf.String(), nil
}

func statusIcon(status string) string {
	switch status {
	case "pending":
		return "[ ]"
	case "in_progress":
		return "[~]"
	case "completed":
		return "[x]"
	default:
		return "[?]"
	}
}

// --- TaskGetTool ---

// TaskGetTool retrieves full details of a task by ID.
type TaskGetTool struct {
	Store        store.Store
	GetSessionID func() string
}

type taskGetInput struct {
	ID string `json:"id"`
}

func (t *TaskGetTool) Name() string { return "task_get" }

func (t *TaskGetTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "task_get",
		Description: "Get full details of a task by its ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id": {"type": "string", "description": "Task ID to retrieve"}
			},
			"required": ["id"]
		}`),
	}
}

func (t *TaskGetTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[taskGetInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}
	if in.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	task, err := t.Store.GetTask(ctx, in.ID)
	if err != nil {
		return "", fmt.Errorf("getting task: %w", err)
	}

	// Verify the task belongs to the caller's session
	sessionID := t.GetSessionID()
	if sessionID != "" && task.SessionID != sessionID {
		return "", fmt.Errorf("task %s not found", in.ID)
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "ID:          %s\n", task.ID)
	fmt.Fprintf(&buf, "Subject:     %s\n", task.Subject)
	fmt.Fprintf(&buf, "Status:      %s\n", task.Status)
	fmt.Fprintf(&buf, "Description: %s\n", task.Description)
	if len(task.BlockedBy) > 0 {
		fmt.Fprintf(&buf, "Blocked by:  %s\n", strings.Join(task.BlockedBy, ", "))
	}

	return buf.String(), nil
}
