package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskOutputTool_Name(t *testing.T) {
	tool := &TaskOutputTool{}
	if tool.Name() != "task_output" {
		t.Errorf("expected 'task_output', got %q", tool.Name())
	}
}

func TestTaskOutputTool_CompletedTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, _ := mgr.Start("echo done", 30)
	time.Sleep(200 * time.Millisecond)

	tool := &TaskOutputTool{Manager: mgr}
	input, _ := json.Marshal(map[string]string{"task_id": taskID})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("expected 'completed' in result, got %q", result)
	}
	if !strings.Contains(result, "done") {
		t.Errorf("expected 'done' in output, got %q", result)
	}
}

func TestTaskOutputTool_InvalidID(t *testing.T) {
	tool := &TaskOutputTool{Manager: &BackgroundTaskManager{
		rootCtx: context.Background(),
		tasks:   make(map[string]*BackgroundTask),
	}}
	input, _ := json.Marshal(map[string]string{"task_id": "bg_nope"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Error("expected error for invalid task ID")
	}
}

func TestTaskOutputTool_RunningTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, _ := mgr.Start("sleep 5", 30)
	// Don't wait — task should be running

	tool := &TaskOutputTool{Manager: mgr}
	input, _ := json.Marshal(map[string]string{"task_id": taskID})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "running") {
		t.Errorf("expected 'running' in result, got %q", result)
	}
}
