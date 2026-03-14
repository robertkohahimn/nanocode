package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type GrepTool struct {
	BaseDir string
}

type grepInput struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Glob            string `json:"glob"`
	CaseInsensitive bool   `json:"case_insensitive"`
}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "grep",
		Description: "Search file contents with a regex pattern. Returns matching lines with file paths and line numbers.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "Regex pattern to search for"},
				"path": {"type": "string", "description": "File or directory to search (default: current directory)"},
				"glob": {"type": "string", "description": "Filter files by glob pattern (e.g. *.go)"},
				"case_insensitive": {"type": "boolean", "description": "Case-insensitive search (default: false)"}
			},
			"required": ["pattern"]
		}`),
	}
}

func (t *GrepTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[grepInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	pat := in.Pattern
	if in.CaseInsensitive {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	root := in.Path
	if root == "" {
		root = "."
	}
	if t.BaseDir != "" {
		baseAbs, err := filepath.EvalSymlinks(t.BaseDir)
		if err != nil {
			return "", fmt.Errorf("resolving base dir: %w", err)
		}
		targetAbs, err := filepath.EvalSymlinks(filepath.Join(baseAbs, root))
		if err != nil {
			return "", fmt.Errorf("resolving path: %w", err)
		}
		rel, err := filepath.Rel(baseAbs, targetAbs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("path %q escapes base directory", root)
		}
		root = targetAbs
	} else if root == "." {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting working directory: %w", err)
		}
	}

	const maxMatches = 100
	var results []string

	// Check if root is a file
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", root, err)
	}

	if !info.IsDir() {
		results = searchFile(root, root, re, maxMatches)
	} else {
		var limitReached bool
		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible entries
			}
			if d.IsDir() {
				if SkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}

			// Apply glob filter using recursive matcher (supports **)
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil // skip entries that cannot be relativized
			}
			rel = filepath.ToSlash(rel)
			if in.Glob != "" {
				if !matchGlob(in.Glob, rel) {
					return nil
				}
			}
			found := searchFile(path, rel, re, maxMatches-len(results))
			results = append(results, found...)

			if len(results) >= maxMatches {
				limitReached = true
				return filepath.SkipAll
			}
			return nil
		})
		if walkErr != nil && !limitReached {
			return "", fmt.Errorf("walking directory: %w", walkErr)
		}
	}

	if len(results) == 0 {
		return "No matches found", nil
	}

	output := strings.Join(results, "\n")
	if len(results) >= maxMatches {
		output += fmt.Sprintf("\n... (truncated at %d matches)", maxMatches)
	}
	return output, nil
}

func searchFile(absPath, displayPath string, re *regexp.Regexp, maxResults int) []string {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Binary check
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if IsBinary(buf[:n]) {
		return nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil
	}

	var results []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() && len(results) < maxResults {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			results = append(results, fmt.Sprintf("%s:%d:%s", displayPath, lineNum, line))
		}
	}
	if err := scanner.Err(); err != nil {
		results = append(results, fmt.Sprintf("%s: scan error: %v", displayPath, err))
	}
	return results
}
