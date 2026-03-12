package tool

import (
	"path/filepath"
	"sync"
)

// FileTracker tracks which files have been read in the current session.
// Tools use this to enforce read-before-edit semantics.
type FileTracker struct {
	mu    sync.RWMutex
	files map[string]bool
}

// NewFileTracker creates an empty FileTracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{files: make(map[string]bool)}
}

// MarkRead records that a file has been read.
// The path is normalized via filepath.Clean, filepath.Abs, and filepath.EvalSymlinks.
func (ft *FileTracker) MarkRead(path string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.files[normalizePath(path)] = true
}

// HasRead returns true if the file has been previously read.
func (ft *FileTracker) HasRead(path string) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.files[normalizePath(path)]
}

// normalizePath cleans and resolves a path to an absolute form.
// It attempts to resolve symlinks for consistency with ValidatePath.
// Falls back to the absolute path if the file doesn't exist yet.
func normalizePath(path string) string {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	// Try to resolve symlinks for consistency with ValidatePath.
	// Fall back to abs path if file doesn't exist yet.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
