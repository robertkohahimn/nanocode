package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanocode/nanocode/internal/provider"
)

type EditTool struct {
	BaseDir string // restrict edits to this directory; empty = no restriction
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

	// Write atomically
	perm := os.FileMode(0644)
	if info, err := os.Stat(in.FilePath); err == nil {
		perm = info.Mode().Perm()
	}
	tmpPath := in.FilePath + ".nanocode.tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), perm); err != nil {
		return "", fmt.Errorf("writing: %w", err)
	}
	if err := os.Rename(tmpPath, in.FilePath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("renaming: %w", err)
	}

	snippet := diffSnippet(newContent, in.NewString)
	return fmt.Sprintf("Edited %s (%d replacement(s))\n%s", filepath.Base(in.FilePath), count, snippet), nil
}

// diffSnippet shows a few lines of context around the replacement in the new content.
func diffSnippet(newContent, newStr string) string {
	idx := strings.Index(newContent, newStr)
	if idx < 0 {
		return ""
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
