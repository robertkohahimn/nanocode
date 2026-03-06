package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type WriteTool struct {
	BaseDir  string              // restrict writes to this directory; empty = no restriction
	OnChange func(filePath string) // called after successful write; nil = no-op
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "write",
		Description: "Write content to a file. Creates parent directories if needed. Overwrites existing content.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the file to write"},
				"content": {"type": "string", "description": "Content to write to the file"}
			},
			"required": ["file_path", "content"]
		}`),
	}
}

func (t *WriteTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[writeInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	if err := ValidatePath(in.FilePath, t.BaseDir); err != nil {
		return "", err
	}

	dir := filepath.Dir(in.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	// Determine file permissions
	perm := os.FileMode(0644)
	if info, err := os.Stat(in.FilePath); err == nil {
		perm = info.Mode().Perm()
	}

	// Write atomically: unique temp file + rename
	tmp, err := os.CreateTemp(dir, filepath.Base(in.FilePath)+".nanocode.*.tmp")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write([]byte(in.Content)); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("setting permissions: %w", err)
	}
	if err := os.Rename(tmpPath, in.FilePath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("renaming temp file: %w", err)
	}

	if t.OnChange != nil {
		t.OnChange(in.FilePath)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), in.FilePath), nil
}
