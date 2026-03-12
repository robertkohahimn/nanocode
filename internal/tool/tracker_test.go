package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEditToolRequiresRead(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")
	os.WriteFile(fp, []byte("hello world"), 0644)

	tracker := NewFileTracker()
	et := &EditTool{Tracker: tracker}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","old_string":"hello","new_string":"goodbye"}`))
	if err == nil {
		t.Fatal("expected error when editing without reading first")
	}
	if !strings.Contains(err.Error(), "must read a file before editing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEditToolAfterRead(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")
	os.WriteFile(fp, []byte("hello world"), 0644)

	tracker := NewFileTracker()
	rt := &ReadTool{Tracker: tracker}
	_, err := rt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`"}`))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	et := &EditTool{Tracker: tracker}
	_, err = et.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","old_string":"hello","new_string":"goodbye"}`))
	if err != nil {
		t.Fatalf("Edit after read should succeed: %v", err)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "goodbye world" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestWriteToolOverwriteRequiresRead(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "existing.txt")
	os.WriteFile(fp, []byte("original"), 0644)

	tracker := NewFileTracker()
	wt := &WriteTool{Tracker: tracker}
	_, err := wt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","content":"new content"}`))
	if err == nil {
		t.Fatal("expected error when overwriting without reading first")
	}
	if !strings.Contains(err.Error(), "must read a file before overwriting") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWriteToolNewFileAllowed(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "brand_new.txt")

	tracker := NewFileTracker()
	wt := &WriteTool{Tracker: tracker}
	_, err := wt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","content":"new file"}`))
	if err != nil {
		t.Fatalf("Writing new file should succeed without read: %v", err)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "new file" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestWriteToolAfterRead(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "existing.txt")
	os.WriteFile(fp, []byte("original"), 0644)

	tracker := NewFileTracker()
	rt := &ReadTool{Tracker: tracker}
	_, err := rt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`"}`))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	wt := &WriteTool{Tracker: tracker}
	_, err = wt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","content":"overwritten"}`))
	if err != nil {
		t.Fatalf("Overwrite after read should succeed: %v", err)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "overwritten" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestFileTrackerSymlink(t *testing.T) {
	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.txt")
	os.WriteFile(realFile, []byte("content"), 0644)

	symlinkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Case 1: MarkRead via symlink, HasRead via real path and symlink
	tracker := NewFileTracker()
	tracker.MarkRead(symlinkPath)
	if !tracker.HasRead(symlinkPath) {
		t.Error("HasRead(symlink) should be true after MarkRead(symlink)")
	}
	if !tracker.HasRead(realFile) {
		t.Error("HasRead(realPath) should be true after MarkRead(symlink)")
	}

	// Case 2: MarkRead via real path, HasRead via symlink
	tracker2 := NewFileTracker()
	tracker2.MarkRead(realFile)
	if !tracker2.HasRead(realFile) {
		t.Error("HasRead(realPath) should be true after MarkRead(realPath)")
	}
	if !tracker2.HasRead(symlinkPath) {
		t.Error("HasRead(symlink) should be true after MarkRead(realPath)")
	}
}

func TestFileTrackerConcurrency(t *testing.T) {
	tracker := NewFileTracker()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		path := fmt.Sprintf("/tmp/file_%d.txt", i)
		go func() {
			defer wg.Done()
			tracker.MarkRead(path)
		}()
		go func() {
			defer wg.Done()
			tracker.HasRead(path)
		}()
	}
	wg.Wait()

	// After all goroutines complete, all files should be marked as read
	for i := 0; i < n; i++ {
		path := fmt.Sprintf("/tmp/file_%d.txt", i)
		if !tracker.HasRead(path) {
			t.Errorf("expected %s to be marked as read", path)
		}
	}
}
