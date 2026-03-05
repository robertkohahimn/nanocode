package store

import (
	"context"
	"testing"
)

func testStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateSession(ctx, "/tmp/project")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	sess, err := s.GetSession(ctx, id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Project != "/tmp/project" {
		t.Errorf("expected project '/tmp/project', got %q", sess.Project)
	}
	if sess.CreatedAt == 0 {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetSession(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestAppendAndGetMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, _ := s.CreateSession(ctx, "/tmp/project")

	msg1 := &MessageRecord{Role: "user", Content: `[{"type":"text","text":"hello"}]`, CreatedAt: 1000}
	msg2 := &MessageRecord{Role: "assistant", Content: `[{"type":"text","text":"hi"}]`, CreatedAt: 1001}

	if err := s.AppendMessage(ctx, id, msg1); err != nil {
		t.Fatalf("AppendMessage 1: %v", err)
	}
	if err := s.AppendMessage(ctx, id, msg2); err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}

	msgs, err := s.GetMessages(ctx, id)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %q", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got %q", msgs[1].Role)
	}
}

func TestListSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s.CreateSession(ctx, "/tmp/project")
	}
	s.CreateSession(ctx, "/tmp/other")

	sessions, err := s.ListSessions(ctx, "/tmp/project", 3)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
}

func TestUpdateSessionTitle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, _ := s.CreateSession(ctx, "/tmp/project")
	if err := s.UpdateSessionTitle(ctx, id, "My Session"); err != nil {
		t.Fatalf("UpdateSessionTitle: %v", err)
	}

	sess, _ := s.GetSession(ctx, id)
	if sess.Title != "My Session" {
		t.Errorf("expected title 'My Session', got %q", sess.Title)
	}
}

func TestAppendMessageUpdatesSessionTime(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, _ := s.CreateSession(ctx, "/tmp/project")
	sess1, _ := s.GetSession(ctx, id)

	msg := &MessageRecord{Role: "user", Content: "[]", CreatedAt: sess1.UpdatedAt + 100}
	s.AppendMessage(ctx, id, msg)

	sess2, _ := s.GetSession(ctx, id)
	if sess2.UpdatedAt <= sess1.UpdatedAt {
		t.Error("expected updated_at to increase after AppendMessage")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	s := testStore(t)
	// Running migrate again should be a no-op
	if err := Migrate(s.db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}
