package engine

import (
	"context"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/store"
)

func TestFailureCollectorNilSafe(t *testing.T) {
	// All methods should be no-ops on a nil collector.
	var fc *FailureCollector
	fc.TrackTool("bash")
	fc.TrackFile("main.go")
	fc.Record(context.Background(), FailureMaxIter, "test", 10)
	// No panic = pass
}

func TestFailureCollectorNilStore(t *testing.T) {
	fc := NewFailureCollector(nil, "session-1")
	if fc != nil {
		t.Error("expected nil collector when store is nil")
	}
}

func TestFailureCollectorEmptySession(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	fc := NewFailureCollector(st, "")
	if fc != nil {
		t.Error("expected nil collector when sessionID is empty")
	}
}

func TestFailureCollectorTrackAndRecord(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	sessID, err := st.CreateSession(ctx, "/tmp/project")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	fc := NewFailureCollector(st, sessID)
	if fc == nil {
		t.Fatal("expected non-nil collector")
	}

	fc.TrackTool("bash")
	fc.TrackTool("read")
	fc.TrackTool("bash") // duplicate should be idempotent
	fc.TrackFile("/tmp/main.go")
	fc.TrackFile("/tmp/util.go")

	fc.Record(ctx, FailureMaxIter, "reached 50 iterations", 50)

	// Verify the record was stored.
	records, err := st.ListFailures(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListFailures: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 failure record, got %d", len(records))
	}

	rec := records[0]
	if rec.SessionID != sessID {
		t.Errorf("expected session %s, got %s", sessID, rec.SessionID)
	}
	if rec.FailureType != "max_iterations" {
		t.Errorf("expected type max_iterations, got %q", rec.FailureType)
	}
	if rec.Iterations != 50 {
		t.Errorf("expected 50 iterations, got %d", rec.Iterations)
	}
	if rec.Description != "reached 50 iterations" {
		t.Errorf("unexpected description: %q", rec.Description)
	}
	// Tools should contain bash and read (2 unique tools).
	if rec.ToolsUsed == "[]" {
		t.Error("expected non-empty tools_used")
	}
	if rec.FilesTouched == "[]" {
		t.Error("expected non-empty files_touched")
	}
}

func TestFailureCollectorMultipleRecords(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	sessID, _ := st.CreateSession(ctx, "/tmp/project")

	fc := NewFailureCollector(st, sessID)
	fc.TrackTool("edit")
	fc.Record(ctx, FailureDoomLoop, "doom loop on main.go", 10)
	fc.Record(ctx, FailureMaxIter, "hit limit", 50)

	records, err := st.ListFailures(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListFailures: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 failure records, got %d", len(records))
	}
}
