package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxStatusLines = 20
	gitTimeout     = 2 * time.Second
	maxProjectCtx  = 1 << 20 // 1MB
)

// BuildProjectContext builds a system prompt suffix with git info,
// environment details, and nanocode.md content for the given project directory.
// Returns an empty string if projectDir is empty.
func BuildProjectContext(projectDir string) string {
	if projectDir == "" {
		return ""
	}

	var sb strings.Builder

	// Git info (only if inside a git repo)
	if isGitRepo(projectDir) {
		if branch := gitCommand(projectDir, "branch", "--show-current"); branch != "" {
			sb.WriteString("Current branch: ")
			sb.WriteString(branch)
			sb.WriteString("\n\n")
		}

		if status := gitCommand(projectDir, "status", "--short"); status != "" {
			lines := strings.Split(status, "\n")
			if len(lines) > maxStatusLines {
				lines = append(lines[:maxStatusLines], fmt.Sprintf("... (%d more)", len(lines)-maxStatusLines))
			}
			sb.WriteString("Status:\n")
			sb.WriteString(strings.Join(lines, "\n"))
			sb.WriteString("\n\n")
		}

		if commits := gitCommand(projectDir, "log", "--oneline", "-5"); commits != "" {
			sb.WriteString("Recent commits:\n")
			sb.WriteString(commits)
			sb.WriteString("\n\n")
		}
	}

	// Environment
	sb.WriteString(fmt.Sprintf("Working directory: %s\n", projectDir))
	sb.WriteString(fmt.Sprintf("Platform: %s\n", runtime.GOOS))
	sb.WriteString(fmt.Sprintf("Date: %s\n", time.Now().Format("2006-01-02")))

	// Project instructions (nanocode.md)
	nanocodePath := filepath.Join(projectDir, "nanocode.md")
	if f, err := os.Open(nanocodePath); err == nil {
		data, readErr := io.ReadAll(io.LimitReader(f, maxProjectCtx+1))
		f.Close()
		if readErr == nil && len(data) > 0 {
			content := string(data)
			if len(data) > maxProjectCtx {
				// Truncate at a valid UTF-8 boundary to avoid splitting multi-byte characters.
				truncated := data[:maxProjectCtx]
				for len(truncated) > 0 && !utf8.Valid(truncated) {
					truncated = truncated[:len(truncated)-1]
				}
				content = string(truncated) + "\n... (truncated at 1MB)"
			}
			sb.WriteString("\n")
			sb.WriteString(content)
		}
	}

	return sb.String()
}

// isGitRepo checks if the directory is inside a git repository.
func isGitRepo(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// gitCommand runs a git command in the given directory and returns
// trimmed stdout. Returns empty string on any error.
func gitCommand(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
