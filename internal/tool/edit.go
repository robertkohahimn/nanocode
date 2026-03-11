package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type EditTool struct {
	BaseDir  string              // restrict edits to this directory; empty = no restriction
	OnChange func(filePath string) // called after successful edit; nil = no-op
}

type editInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (t *EditTool) Name() string { return "edit" }

func (t *EditTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "edit",
		Description: "Edit a file by replacing old_string with new_string. The old_string must match exactly and uniquely (unless replace_all is true).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the file to edit"},
				"old_string": {"type": "string", "description": "Exact string to find in the file"},
				"new_string": {"type": "string", "description": "String to replace it with"},
				"replace_all": {"type": "boolean", "description": "Replace all occurrences (default false)", "default": false}
			},
			"required": ["file_path", "old_string", "new_string"]
		}`),
	}
}

func (t *EditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[editInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	if err := ValidatePath(in.FilePath, t.BaseDir); err != nil {
		return "", err
	}

	if in.OldString == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, in.OldString)

	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", in.FilePath)
	}
	if count > 1 && !in.ReplaceAll {
		return "", fmt.Errorf("old_string found %d times in %s; provide more context to make it unique, or set replace_all to true", count, in.FilePath)
	}

	var newContent string
	if in.ReplaceAll {
		newContent = strings.ReplaceAll(content, in.OldString, in.NewString)
	} else {
		newContent = strings.Replace(content, in.OldString, in.NewString, 1)
	}

	// Write atomically with unique temp file
	perm := os.FileMode(0644)
	if info, err := os.Stat(in.FilePath); err == nil {
		perm = info.Mode().Perm()
	}
	dir := filepath.Dir(in.FilePath)
	tmp, err := os.CreateTemp(dir, filepath.Base(in.FilePath)+".nanocode.*.tmp")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write([]byte(newContent)); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing: %w", err)
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
		return "", fmt.Errorf("renaming: %w", err)
	}

	if t.OnChange != nil {
		t.OnChange(in.FilePath)
	}

	// Compute the position where the replacement occurred for diffSnippet
	replacePos := strings.Index(content, in.OldString)
	snippet := diffSnippet(newContent, in.NewString, replacePos)
	return fmt.Sprintf("Edited %s (%d replacement(s))\n%s", filepath.Base(in.FilePath), count, snippet), nil
}

// diffSnippet shows a few lines of context around the replacement in the new content.
// replacePos is the position in the original content where the replacement occurred,
// used when newStr is empty (deletion) since strings.Index would return 0.
func diffSnippet(newContent, newStr string, replacePos int) string {
	var idx int
	if newStr == "" {
		// For deletions, use the provided position (clamped to content bounds)
		idx = replacePos
		if idx > len(newContent) {
			idx = len(newContent)
		}
		if idx < 0 {
			idx = 0
		}
	} else {
		idx = strings.Index(newContent, newStr)
		if idx < 0 {
			return ""
		}
	}

	lines := strings.Split(newContent, "\n")
	// Find which line the change starts on
	charCount := 0
	startLine := 0
	for i, line := range lines {
		if charCount+len(line)+1 > idx {
			startLine = i
			break
		}
		charCount += len(line) + 1
	}

	// Show 3 lines before and after the replacement
	from := startLine - 3
	if from < 0 {
		from = 0
	}
	newLines := strings.Split(newStr, "\n")
	to := startLine + len(newLines) + 3
	if to > len(lines) {
		to = len(lines)
	}

	var buf strings.Builder
	for i := from; i < to; i++ {
		fmt.Fprintf(&buf, " %4d | %s\n", i+1, lines[i])
	}
	return buf.String()
}
