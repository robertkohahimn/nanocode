package snapshot

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/robertkohahimn/nanocode/internal/store"
)

// Manager tracks file changes by creating git commits and storing snapshot records.
type Manager struct {
	projectDir string
	store      store.Store
	sessionID  string
	mu         sync.Mutex
	trackMu    sync.Mutex // serializes git add/commit/rev-parse
}

// New creates a snapshot manager. Call SetSession before Track will do anything.
// Panics if st is nil.
func New(projectDir string, st store.Store) *Manager {
	if st == nil {
		panic("snapshot.New: store must not be nil")
	}
	return &Manager{
		projectDir: projectDir,
		store:      st,
	}
}

// SetSession sets the session ID for snapshot tracking.
// Called by the engine when a session is known.
func (m *Manager) SetSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionID = id
}

// Track creates a git commit for the given file and records a snapshot.
// Safe to call concurrently. Silently no-ops if no session is set or
// if any git operation fails.
func (m *Manager) Track(filePath string) {
	m.mu.Lock()
	sessionID := m.sessionID
	projectDir := m.projectDir
	st := m.store
	m.mu.Unlock()

	if sessionID == "" {
		return
	}

	m.trackMu.Lock()
	defer m.trackMu.Unlock()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Printf("snapshot: abs path: %v", err)
		return
	}

	filename := filepath.Base(absPath)
	prefix := sessionID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	// Stage the file
	addCmd := exec.Command("git", "add", "--", absPath)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		log.Printf("snapshot: git add: %s %v", strings.TrimSpace(string(out)), err)
		return
	}

	// Commit with direct exec to avoid shell injection via filename
	msg := fmt.Sprintf("nanocode: %s [session:%s]", filename, prefix)
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	out, err := commitCmd.CombinedOutput()
	if err != nil {
		// "nothing to commit" is not an error we care about
		if strings.Contains(string(out), "nothing to commit") {
			return
		}
		log.Printf("snapshot: git commit: %s %v", strings.TrimSpace(string(out)), err)
		return
	}

	// Get commit hash
	hashCmd := exec.Command("git", "rev-parse", "HEAD")
	hashCmd.Dir = projectDir
	hashOut, err := hashCmd.Output()
	if err != nil {
		log.Printf("snapshot: git rev-parse: %v", err)
		return
	}
	gitHash := strings.TrimSpace(string(hashOut))
	if len(gitHash) < 7 {
		log.Printf("snapshot: unexpected git rev-parse output: %s", string(hashOut))
		return
	}

	// Store snapshot record (fire-and-forget, use background context)
	snap := &store.SnapshotRecord{
		SessionID: sessionID,
		FilePath:  absPath,
		GitHash:   gitHash,
	}
	if err := st.CreateSnapshot(context.Background(), snap); err != nil {
		log.Printf("snapshot: store: %v", err)
	}
}
