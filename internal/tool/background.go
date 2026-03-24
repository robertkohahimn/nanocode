package tool

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackgroundTask tracks a command running in the background.
type BackgroundTask struct {
	ID         string
	Command    string
	Status     string // "running", "completed", "failed"
	OutputPath string
	ExitCode   int
	StartedAt  time.Time
	DoneAt     time.Time
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// BackgroundTaskManager tracks background bash commands.
type BackgroundTaskManager struct {
	rootCtx context.Context
	mu      sync.Mutex
	tasks   map[string]*BackgroundTask
}

// NewBackgroundTaskManager creates a manager. It scans for and removes
// stale temp files (>24h old) from previous crashed sessions.
func NewBackgroundTaskManager(rootCtx context.Context) *BackgroundTaskManager {
	cleanStaleFiles()
	return &BackgroundTaskManager{
		rootCtx: rootCtx,
		tasks:   make(map[string]*BackgroundTask),
	}
}

// Start launches a command in the background and returns its task ID.
func (m *BackgroundTaskManager) Start(command string, timeout int) (string, error) {
	id, err := generateTaskID()
	if err != nil {
		return "", fmt.Errorf("generating task ID: %w", err)
	}

	tmpDir := filepath.Join(os.TempDir(), "nanocode")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	outPath := filepath.Join(tmpDir, id+".out")
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("creating output file: %w", err)
	}

	if timeout <= 0 {
		timeout = 300
	}
	if timeout > 3600 {
		timeout = 3600
	}

	taskCtx, cancel := context.WithTimeout(m.rootCtx, time.Duration(timeout)*time.Second)

	task := &BackgroundTask{
		ID:         id,
		Command:    command,
		Status:     "running",
		OutputPath: outPath,
		StartedAt:  time.Now(),
		cancel:     cancel,
	}

	m.mu.Lock()
	m.tasks[id] = task
	m.mu.Unlock()

	go func() {
		defer outFile.Close()
		defer cancel()

		cmd := exec.CommandContext(taskCtx, "bash", "-c", command)
		cmd.Stdout = outFile
		cmd.Stderr = outFile

		err := cmd.Run()
		task.mu.Lock()
		defer task.mu.Unlock()
		task.DoneAt = time.Now()
		if err != nil {
			task.Status = "failed"
			if exitErr, ok := err.(*exec.ExitError); ok {
				task.ExitCode = exitErr.ExitCode()
			} else {
				task.ExitCode = -1
			}
		} else {
			task.Status = "completed"
			task.ExitCode = 0
		}
	}()

	return id, nil
}

// Get returns a task by ID, or nil if not found.
func (m *BackgroundTaskManager) Get(id string) *BackgroundTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id]
}

// ReadOutput returns the current output of a background task.
func (m *BackgroundTaskManager) ReadOutput(id string) (string, error) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("background task not found: %s", id)
	}
	f, err := os.Open(task.OutputPath)
	if err != nil {
		return "", fmt.Errorf("reading output: %w", err)
	}
	defer f.Close()
	// Read at most MaxOutputLen+margin to avoid loading arbitrarily large files.
	buf := make([]byte, MaxOutputLen+64)
	n, _ := io.ReadFull(f, buf)
	return TruncateOutput(string(buf[:n]), MaxOutputLen), nil
}

// Cleanup cancels running tasks and removes temp files.
func (m *BackgroundTaskManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, task := range m.tasks {
		if task.cancel != nil {
			task.cancel()
		}
		os.Remove(task.OutputPath)
	}
	m.tasks = make(map[string]*BackgroundTask)
}

func generateTaskID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("bg_%x", b), nil
}

func cleanStaleFiles() {
	tmpDir := filepath.Join(os.TempDir(), "nanocode")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "bg_") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(tmpDir, e.Name()))
		}
	}
}
