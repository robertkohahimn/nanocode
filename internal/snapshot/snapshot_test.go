package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/store"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", args, out, err)
		}
	}

	// Create an initial commit so HEAD exists
	initFile := filepath.Join(dir, "init.txt")
	os.WriteFile(initFile, []byte("init"), 0644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.CombinedOutput()
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit: %s %v", out, err)
	}

	return dir
}

func TestManager_Track(t *testing.T) {
	dir := setupGitRepo(t)

	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	sessionID, err := st.CreateSession(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	mgr := New(dir, st)
	mgr.SetSession(sessionID)

	// Write a file and track it
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr.Track(testFile)

	// Verify snapshot was recorded
	snaps, err := st.ListSnapshots(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].FilePath != testFile {
		t.Errorf("file path: got %q, want %q", snaps[0].FilePath, testFile)
	}
	if len(snaps[0].GitHash) < 7 {
		t.Errorf("git hash too short: %q", snaps[0].GitHash)
	}

	// Verify git commit exists
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %s %v", out, err)
	}
	if got := string(out); !contains(got, "nanocode:") {
		t.Errorf("commit message should contain 'nanocode:': %s", got)
	}
}

func TestManager_NoSession(t *testing.T) {
	dir := setupGitRepo(t)

	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mgr := New(dir, st)
	// Don't call SetSession

	testFile := filepath.Join(dir, "noop.txt")
	os.WriteFile(testFile, []byte("noop"), 0644)

	// Should silently no-op
	mgr.Track(testFile)

	// Verify no git commit was made (only the initial commit)
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if got := string(out); !contains(got, "1") {
		t.Errorf("expected 1 commit (initial only), got: %s", got)
	}
}

func TestManager_MultipleTracksOnSameFile(t *testing.T) {
	dir := setupGitRepo(t)

	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, dir)
	mgr := New(dir, st)
	mgr.SetSession(sessionID)

	testFile := filepath.Join(dir, "multi.txt")

	// First write + track
	os.WriteFile(testFile, []byte("v1"), 0644)
	mgr.Track(testFile)

	// Second write + track
	os.WriteFile(testFile, []byte("v2"), 0644)
	mgr.Track(testFile)

	snaps, _ := st.ListSnapshots(ctx, sessionID)
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	if snaps[0].GitHash == snaps[1].GitHash {
		t.Error("snapshots should have different git hashes")
	}
}

func TestManager_NothingToCommit(t *testing.T) {
	dir := setupGitRepo(t)

	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, dir)
	mgr := New(dir, st)
	mgr.SetSession(sessionID)

	// Track an already-committed file with no changes
	initFile := filepath.Join(dir, "init.txt")
	mgr.Track(initFile)

	// Should produce no snapshot
	snaps, _ := st.ListSnapshots(ctx, sessionID)
	if len(snaps) != 0 {
		t.Errorf("expected 0 snapshots for unchanged file, got %d", len(snaps))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
