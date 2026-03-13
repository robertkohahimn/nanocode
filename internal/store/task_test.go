package store

import (
	"context"
	"testing"
)

func TestCreateAndGetTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")

	id, err := s.CreateTask(ctx, sessID, "Fix the bug", "There is a nil pointer in main.go")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty task ID")
	}

	task, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Subject != "Fix the bug" {
		t.Errorf("expected subject 'Fix the bug', got %q", task.Subject)
	}
	if task.Description != "There is a nil pointer in main.go" {
		t.Errorf("unexpected description: %q", task.Description)
	}
	if task.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", task.Status)
	}
	if task.SessionID != sessID {
		t.Errorf("expected session %q, got %q", sessID, task.SessionID)
	}
	if task.CreatedAt == 0 {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestGetTaskNotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetTask(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestUpdateTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")
	id, _ := s.CreateTask(ctx, sessID, "Original", "desc")

	status := "in_progress"
	subject := "Updated subject"
	err := s.UpdateTask(ctx, id, TaskUpdate{Status: &status, Subject: &subject})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	task, _ := s.GetTask(ctx, id)
	if task.Status != "in_progress" {
		t.Errorf("expected 'in_progress', got %q", task.Status)
	}
	if task.Subject != "Updated subject" {
		t.Errorf("expected 'Updated subject', got %q", task.Subject)
	}
}

func TestUpdateTaskNotFound(t *testing.T) {
	s := testStore(t)
	status := "completed"
	err := s.UpdateTask(context.Background(), "nonexistent", TaskUpdate{Status: &status})
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestListTasks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")
	otherSessID, _ := s.CreateSession(ctx, "/tmp/other")

	s.CreateTask(ctx, sessID, "Task 1", "")
	s.CreateTask(ctx, sessID, "Task 2", "")
	s.CreateTask(ctx, otherSessID, "Other task", "")

	tasks, err := s.ListTasks(ctx, sessID)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Subject != "Task 1" {
		t.Errorf("expected 'Task 1', got %q", tasks[0].Subject)
	}
}

func TestDeleteTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")
	id, _ := s.CreateTask(ctx, sessID, "To delete", "")

	err := s.DeleteTask(ctx, id)
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	_, err = s.GetTask(ctx, id)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestDeleteTaskNotFound(t *testing.T) {
	s := testStore(t)
	err := s.DeleteTask(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestAddTaskDep(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")
	id1, _ := s.CreateTask(ctx, sessID, "Task 1", "")
	id2, _ := s.CreateTask(ctx, sessID, "Task 2", "")

	err := s.AddTaskDep(ctx, id2, id1)
	if err != nil {
		t.Fatalf("AddTaskDep: %v", err)
	}

	task, _ := s.GetTask(ctx, id2)
	if len(task.BlockedBy) != 1 || task.BlockedBy[0] != id1 {
		t.Errorf("expected blocked by [%s], got %v", id1, task.BlockedBy)
	}
}

func TestAddTaskDepIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")
	id1, _ := s.CreateTask(ctx, sessID, "Task 1", "")
	id2, _ := s.CreateTask(ctx, sessID, "Task 2", "")

	s.AddTaskDep(ctx, id2, id1)
	// Adding same dep again should not error (INSERT OR IGNORE)
	err := s.AddTaskDep(ctx, id2, id1)
	if err != nil {
		t.Fatalf("second AddTaskDep should not error: %v", err)
	}

	task, _ := s.GetTask(ctx, id2)
	if len(task.BlockedBy) != 1 {
		t.Errorf("expected 1 dep, got %d", len(task.BlockedBy))
	}
}

func TestListTasksWithDeps(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")
	id1, _ := s.CreateTask(ctx, sessID, "Task 1", "")
	id2, _ := s.CreateTask(ctx, sessID, "Task 2", "")
	s.AddTaskDep(ctx, id2, id1)

	tasks, err := s.ListTasks(ctx, sessID)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// Task 2 should have deps
	var task2 Task
	for _, task := range tasks {
		if task.ID == id2 {
			task2 = task
		}
	}
	if len(task2.BlockedBy) != 1 || task2.BlockedBy[0] != id1 {
		t.Errorf("expected task 2 blocked by task 1, got %v", task2.BlockedBy)
	}
}
