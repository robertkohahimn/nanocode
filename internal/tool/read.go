package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type ReadTool struct {
	BaseDir string // restrict reads to this directory; empty = no restriction
}

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "read",
		Description: "Read file contents. Returns numbered lines. Use offset/limit for large files.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the file to read"},
				"offset": {"type": "integer", "description": "Start line number (1-indexed, default: 1)"},
				"limit": {"type": "integer", "description": "Maximum number of lines to return (default: all)"}
			},
			"required": ["file_path"]
		}`),
	}
}

func (t *ReadTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[readInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	if err := ValidatePath(in.FilePath, t.BaseDir); err != nil {
		return "", err
	}

	const maxReadBytes = 1 << 20 // 1 MiB hard cap
	f, err := os.Open(in.FilePath)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxReadBytes)+1))
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	if len(data) > maxReadBytes {
		return "", fmt.Errorf("file too large: exceeds %d bytes", maxReadBytes)
	}

	if IsBinary(data) {
		return fmt.Sprintf("Binary file: %s (%d bytes)", in.FilePath, len(data)), nil
	}

	lines := strings.Split(string(data), "\n")

	// Apply offset (1-indexed)
	start := 0
	if in.Offset > 0 {
		start = in.Offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}

	// Apply limit
	end := len(lines)
	if in.Limit > 0 && start+in.Limit < end {
		end = start + in.Limit
	}

	var buf strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&buf, "%6d\t%s\n", i+1, lines[i])
	}

	return TruncateOutput(buf.String(), MaxOutputLen), nil
}
