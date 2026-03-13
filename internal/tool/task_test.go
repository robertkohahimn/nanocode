package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/store"
)

func testTaskStore(t *testing.T) store.Store {
	t.Helper()
	s, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testSessionID(t *testing.T, s store.Store) string {
	t.Helper()
	id, err := s.CreateSession(context.Background(), "/tmp/project")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return id
}

func TestTaskCreateToolBasic(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)

	tool := &TaskCreateTool{Store: s, GetSessionID: func() string { return sessID }}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"subject":"Fix tests","description":"Unit tests are failing"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Created task") {
		t.Errorf("unexpected result: %q", result)
	}
	if !strings.Contains(result, "Fix tests") {
		t.Errorf("result should contain subject: %q", result)
	}
}

func TestTaskCreateToolNoSession(t *testing.T) {
	s := testTaskStore(t)
	tool := &TaskCreateTool{Store: s, GetSessionID: func() string { return "" }}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"subject":"test"}`))
	if err == nil {
		t.Fatal("expected error with no session")
	}
}

func TestTaskCreateToolEmptySubject(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)
	tool := &TaskCreateTool{Store: s, GetSessionID: func() string { return sessID }}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"subject":""}`))
	if err == nil {
		t.Fatal("expected error for empty subject")
	}
}

func TestTaskUpdateToolStatus(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)
	ctx := context.Background()

	id, _ := s.CreateTask(ctx, sessID, "My task", "")

	tool := &TaskUpdateTool{Store: s, GetSessionID: func() string { return sessID }}
	result, err := tool.Execute(ctx, json.RawMessage(`{"id":"`+id+`","status":"in_progress"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Updated task") {
		t.Errorf("unexpected result: %q", result)
	}

	task, _ := s.GetTask(ctx, id)
	if task.Status != "in_progress" {
		t.Errorf("expected 'in_progress', got %q", task.Status)
	}
}

func TestTaskUpdateToolDelete(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)
	ctx := context.Background()

	id, _ := s.CreateTask(ctx, sessID, "To delete", "")

	tool := &TaskUpdateTool{Store: s, GetSessionID: func() string { return sessID }}
	result, err := tool.Execute(ctx, json.RawMessage(`{"id":"`+id+`","status":"deleted"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Deleted task") {
		t.Errorf("unexpected result: %q", result)
	}

	_, err = s.GetTask(ctx, id)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestTaskUpdateToolBlockedBy(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)
	ctx := context.Background()

	id1, _ := s.CreateTask(ctx, sessID, "Task 1", "")
	id2, _ := s.CreateTask(ctx, sessID, "Task 2", "")

	tool := &TaskUpdateTool{Store: s, GetSessionID: func() string { return sessID }}
	_, err := tool.Execute(ctx, json.RawMessage(`{"id":"`+id2+`","blocked_by":["`+id1+`"]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	task, _ := s.GetTask(ctx, id2)
	if len(task.BlockedBy) != 1 || task.BlockedBy[0] != id1 {
		t.Errorf("expected blocked by [%s], got %v", id1, task.BlockedBy)
	}
}

func TestTaskListToolBasic(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)
	ctx := context.Background()

	s.CreateTask(ctx, sessID, "Task A", "")
	s.CreateTask(ctx, sessID, "Task B", "")

	tool := &TaskListTool{Store: s, GetSessionID: func() string { return sessID }}
	result, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Task A") || !strings.Contains(result, "Task B") {
		t.Errorf("expected both tasks in result: %q", result)
	}
	if !strings.Contains(result, "[pending]") {
		t.Errorf("expected status indicators: %q", result)
	}
}

func TestTaskListToolEmpty(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)

	tool := &TaskListTool{Store: s, GetSessionID: func() string { return sessID }}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "No tasks") {
		t.Errorf("expected 'No tasks' message, got %q", result)
	}
}

func TestTaskGetToolBasic(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)
	ctx := context.Background()

	id, _ := s.CreateTask(ctx, sessID, "My task", "Detailed description")

	tool := &TaskGetTool{Store: s, GetSessionID: func() string { return sessID }}
	result, err := tool.Execute(ctx, json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "My task") {
		t.Errorf("expected subject in result: %q", result)
	}
	if !strings.Contains(result, "Detailed description") {
		t.Errorf("expected description in result: %q", result)
	}
	if !strings.Contains(result, "pending") {
		t.Errorf("expected status in result: %q", result)
	}
}

func TestTaskGetToolNotFound(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)

	tool := &TaskGetTool{Store: s, GetSessionID: func() string { return sessID }}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"id":"nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestTaskGetToolEmptyID(t *testing.T) {
	s := testTaskStore(t)
	sessID := testSessionID(t, s)

	tool := &TaskGetTool{Store: s, GetSessionID: func() string { return sessID }}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"id":""}`))
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}
