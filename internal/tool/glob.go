package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type GlobTool struct{}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "glob",
		Description: "Find files matching a glob pattern. Supports ** for recursive matching.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "Glob pattern (e.g. **/*.go, src/*.ts)"},
				"path": {"type": "string", "description": "Directory to search in (default: current directory)"}
			},
			"required": ["pattern"]
		}`),
	}
}

func (t *GlobTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[globInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	root := in.Path
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting working directory: %w", err)
		}
	}

	const maxResults = 200
	var matches []string
	var limitReached bool

	// Normalize pattern to forward slashes for consistent cross-platform matching
	pattern := filepath.ToSlash(in.Pattern)

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip individual entry errors
		}
		if d.IsDir() && SkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil // skip paths that can't be made relative
		}
		// Normalize to forward slashes for cross-platform glob matching
		rel = filepath.ToSlash(rel)
		if matchGlob(pattern, rel) {
			matches = append(matches, rel)
			if len(matches) >= maxResults {
				limitReached = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil && !limitReached {
		return "", fmt.Errorf("walking directory: %w", walkErr)
	}

	sort.Strings(matches)

	if len(matches) == 0 {
		return "No files matched", nil
	}

	result := strings.Join(matches, "\n")
	if limitReached {
		result += fmt.Sprintf("\n... (truncated at %d results)", maxResults)
	}
	return result, nil
}

// matchGlob matches a path against a pattern that may contain ** (doublestar).
// ** matches any number of directory levels (including zero).
// Supports multiple ** segments (e.g., src/**/internal/**/*.go).
func matchGlob(pattern, path string) bool {
	// If no doublestar, use filepath.Match directly
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Split on first ** and handle recursively
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/"+string(filepath.Separator))
	suffix := strings.TrimPrefix(parts[1], "/")
	suffix = strings.TrimPrefix(suffix, string(filepath.Separator))

	// Prefix must match the start of the path
	if prefix != "" {
		if !strings.HasPrefix(path, prefix+"/") && path != prefix {
			return false
		}
		// Strip matched prefix from path for recursive matching
		path = strings.TrimPrefix(path, prefix+"/")
	}

	// If no suffix, ** at end matches everything
	if suffix == "" {
		return true
	}

	// Try matching suffix against every possible tail of the path.
	// This handles ** matching zero or more directory levels.
	pathParts := strings.Split(path, "/")
	for i := 0; i <= len(pathParts); i++ {
		tail := strings.Join(pathParts[i:], "/")
		// Recurse: suffix may contain more ** segments
		if matchGlob(suffix, tail) {
			return true
		}
	}
	return false
}
