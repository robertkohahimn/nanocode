package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackgroundTaskManager_StartAndGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, err := mgr.Start("echo hello", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(taskID, "bg_") {
		t.Errorf("expected bg_ prefix, got %q", taskID)
	}

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	task := mgr.Get(taskID)
	if task == nil {
		t.Fatal("expected task, got nil")
	}
	if task.Status != "completed" {
		t.Errorf("expected completed, got %q", task.Status)
	}
	if task.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", task.ExitCode)
	}
}

func TestBackgroundTaskManager_ReadOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, err := mgr.Start("echo hello", 30)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	output, err := mgr.ReadOutput(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello' in output, got %q", output)
	}
}

func TestBackgroundTaskManager_InvalidID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	if task := mgr.Get("bg_nonexistent"); task != nil {
		t.Error("expected nil for invalid ID")
	}
	if _, err := mgr.ReadOutput("bg_nonexistent"); err == nil {
		t.Error("expected error for invalid ID")
	}
}

func TestBackgroundTaskManager_FailedCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	taskID, err := mgr.Start("exit 1", 30)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	task := mgr.Get(taskID)
	if task.Status != "failed" {
		t.Errorf("expected failed, got %q", task.Status)
	}
	if task.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", task.ExitCode)
	}
}

func TestBackgroundTaskManager_Cleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)

	taskID, _ := mgr.Start("echo cleanup", 30)
	time.Sleep(200 * time.Millisecond)

	task := mgr.Get(taskID)
	outputPath := task.OutputPath

	mgr.Cleanup()

	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("expected temp file removed after cleanup")
	}
}

func TestBackgroundTaskManager_StaleFileCleanup(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "nanocode")
	os.MkdirAll(tmpDir, 0755)

	// Create a fake stale file
	stalePath := filepath.Join(tmpDir, "bg_stale1234.out")
	os.WriteFile(stalePath, []byte("old"), 0644)
	// Set mtime to 25 hours ago
	oldTime := time.Now().Add(-25 * time.Hour)
	os.Chtimes(stalePath, oldTime, oldTime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewBackgroundTaskManager(ctx)
	defer mgr.Cleanup()

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("expected stale file removed on construction")
	}
}
