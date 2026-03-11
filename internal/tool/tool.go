package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// Tool is the interface every built-in tool implements.
type Tool interface {
	// Name returns the tool identifier (matches ToolDef.Name).
	Name() string

	// Definition returns the JSON Schema tool definition sent to the LLM.
	Definition() provider.ToolDef

	// Execute runs the tool with the given JSON input.
	// Returns the result string or an error.
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// ParseInput unmarshals JSON input into the target struct.
func ParseInput[T any](input json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(input, &v)
	return v, err
}

// MaxOutputLen is the default truncation limit for tool output (100KB).
const MaxOutputLen = 100 * 1024

// TruncateOutput caps tool output at maxLen runes (not bytes) to preserve UTF-8.
func TruncateOutput(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "\n... (truncated)"
}

// IsBinary checks if data likely contains binary content
// by looking for null bytes in the first 512 bytes.
func IsBinary(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

// ValidatePath checks that filePath is within baseDir.
// Returns nil if baseDir is empty (no restriction).
func ValidatePath(filePath, baseDir string) error {
	if baseDir == "" {
		return nil
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("invalid base dir: %w", err)
	}

	// Resolve symlinks to prevent symlink-based sandbox escapes.
	// Resolve the parent directory of the target (the file may not exist yet).
	resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}
	resolved := filepath.Join(resolvedDir, filepath.Base(abs))

	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return fmt.Errorf("resolving base dir: %w", err)
	}

	if !strings.HasPrefix(resolved, resolvedBase+string(filepath.Separator)) && resolved != resolvedBase {
		return fmt.Errorf("path %s is outside project directory %s", filePath, baseDir)
	}
	return nil
}

// SkipDir returns true for directories that should be skipped during traversal.
func SkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".nanocode", "__pycache__", ".venv":
		return true
	}
	return false
}
