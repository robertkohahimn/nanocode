package store

import (
	"context"
	"testing"
	"time"
)

func TestCreateAndGetFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, err := s.CreateSession(ctx, "/tmp/project")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := &FailureRecord{
		SessionID:    sessID,
		Timestamp:    time.Now().Unix(),
		FailureType:  "max_iterations",
		Description:  "reached 50 iterations",
		ToolsUsed:    `["bash","read"]`,
		FilesTouched: `["main.go"]`,
		Iterations:   50,
		Notes:        "",
	}

	if err := s.CreateFailure(ctx, rec); err != nil {
		t.Fatalf("CreateFailure: %v", err)
	}
	if rec.ID == "" {
		t.Fatal("expected ID to be set")
	}

	got, err := s.GetFailure(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetFailure: %v", err)
	}
	if got.FailureType != "max_iterations" {
		t.Errorf("expected type max_iterations, got %q", got.FailureType)
	}
	if got.Iterations != 50 {
		t.Errorf("expected 50 iterations, got %d", got.Iterations)
	}
	if got.Description != "reached 50 iterations" {
		t.Errorf("unexpected description: %q", got.Description)
	}
}

func TestCreateFailureDefaults(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")

	rec := &FailureRecord{
		SessionID:   sessID,
		FailureType: "timeout",
	}
	if err := s.CreateFailure(ctx, rec); err != nil {
		t.Fatalf("CreateFailure: %v", err)
	}
	if rec.ID == "" {
		t.Fatal("expected auto-generated ID")
	}
	if rec.Timestamp == 0 {
		t.Fatal("expected auto-generated Timestamp")
	}

	got, err := s.GetFailure(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetFailure: %v", err)
	}
	if got.ToolsUsed != "[]" {
		t.Errorf("expected default ToolsUsed '[]', got %q", got.ToolsUsed)
	}
	if got.FilesTouched != "[]" {
		t.Errorf("expected default FilesTouched '[]', got %q", got.FilesTouched)
	}
}

func TestListFailuresSinceFilter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")

	now := time.Now().Unix()
	old := &FailureRecord{
		SessionID:   sessID,
		Timestamp:   now - 86400*10, // 10 days ago
		FailureType: "old",
	}
	recent := &FailureRecord{
		SessionID:   sessID,
		Timestamp:   now - 3600, // 1 hour ago
		FailureType: "recent",
	}

	if err := s.CreateFailure(ctx, old); err != nil {
		t.Fatalf("CreateFailure old: %v", err)
	}
	if err := s.CreateFailure(ctx, recent); err != nil {
		t.Fatalf("CreateFailure recent: %v", err)
	}

	// List since 1 day ago: should only get the recent one
	since := now - 86400
	records, err := s.ListFailures(ctx, since, 100)
	if err != nil {
		t.Fatalf("ListFailures: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].FailureType != "recent" {
		t.Errorf("expected 'recent', got %q", records[0].FailureType)
	}

	// List since 0: should get both
	all, err := s.ListFailures(ctx, 0, 100)
	if err != nil {
		t.Fatalf("ListFailures all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 records, got %d", len(all))
	}
}

func TestListFailuresLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")

	for i := 0; i < 5; i++ {
		rec := &FailureRecord{
			SessionID:   sessID,
			FailureType: "test",
			Timestamp:   time.Now().Unix() + int64(i),
		}
		if err := s.CreateFailure(ctx, rec); err != nil {
			t.Fatalf("CreateFailure %d: %v", i, err)
		}
	}

	records, err := s.ListFailures(ctx, 0, 3)
	if err != nil {
		t.Fatalf("ListFailures: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records with limit, got %d", len(records))
	}
}

func TestAnnotateFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sessID, _ := s.CreateSession(ctx, "/tmp/project")

	rec := &FailureRecord{
		SessionID:   sessID,
		FailureType: "unknown",
		Timestamp:   time.Now().Unix(),
	}
	if err := s.CreateFailure(ctx, rec); err != nil {
		t.Fatalf("CreateFailure: %v", err)
	}

	if err := s.AnnotateFailure(ctx, rec.ID, "doom_loop", "infinite edit cycle"); err != nil {
		t.Fatalf("AnnotateFailure: %v", err)
	}

	got, _ := s.GetFailure(ctx, rec.ID)
	if got.FailureType != "doom_loop" {
		t.Errorf("expected type doom_loop, got %q", got.FailureType)
	}
	if got.Notes != "infinite edit cycle" {
		t.Errorf("expected notes 'infinite edit cycle', got %q", got.Notes)
	}
}

func TestAnnotateFailureNotFound(t *testing.T) {
	s := testStore(t)
	err := s.AnnotateFailure(context.Background(), "nonexistent", "x", "y")
	if err == nil {
		t.Fatal("expected error for nonexistent failure")
	}
}

func TestCreateFailureNil(t *testing.T) {
	s := testStore(t)
	err := s.CreateFailure(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil record")
	}
}

func TestMarshalStringSlice(t *testing.T) {
	got := MarshalStringSlice([]string{"a", "b"})
	if got != `["a","b"]` {
		t.Errorf("unexpected: %s", got)
	}
	got = MarshalStringSlice(nil)
	if got != `[]` {
		t.Errorf("expected '[]' for nil, got %s", got)
	}
}
