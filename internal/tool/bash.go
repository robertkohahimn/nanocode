package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

type BashTool struct {
	// ConfirmFunc is called before executing a command.
	// Return true to allow execution. Default: interactive Y/n prompt on stderr.
	ConfirmFunc func(command string) bool
	stdinReader *bufio.Reader
}

type bashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // seconds
}

// NewBashTool creates a BashTool. If stdinReader is non-nil it is used for
// confirmation prompts, avoiding conflicts with other buffered readers on stdin.
func NewBashTool(stdinReader *bufio.Reader) *BashTool {
	bt := &BashTool{stdinReader: stdinReader}
	bt.ConfirmFunc = bt.defaultConfirm
	return bt
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        "bash",
		Description: "Execute a shell command. The user will be asked to confirm before execution.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "The shell command to execute"},
				"timeout": {"type": "integer", "description": "Timeout in seconds (default: 30, max: 300)"}
			},
			"required": ["command"]
		}`),
	}
}

func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := ParseInput[bashInput](input)
	if err != nil {
		return "", fmt.Errorf("parsing input: %w", err)
	}

	// Confirm with user
	confirm := t.ConfirmFunc
	if confirm == nil {
		confirm = t.defaultConfirm
	}
	if !confirm(in.Command) {
		return "Command rejected by user", nil
	}

	// Set timeout
	timeout := 30
	if in.Timeout > 0 {
		timeout = in.Timeout
	}
	if timeout > 300 {
		timeout = 300
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", in.Command)
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	cmd.Dir = wd

	output, err := cmd.CombinedOutput()
	result := string(output)

	// Determine exit status for feedback
	exitCode := 0
	timedOut := false
	if err != nil {
		timedOut = cmdCtx.Err() == context.DeadlineExceeded
		if timedOut {
			result += fmt.Sprintf("\n(timed out after %ds)", timeout)
		}
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		result = fmt.Sprintf("Exit code %d\n%s", exitCode, result)
	}

	// Print visual feedback to stderr
	fmt.Fprintln(os.Stderr, formatCommandFeedback(string(output), exitCode, timedOut))

	return TruncateOutput(result, MaxOutputLen), nil
}

func (t *BashTool) defaultConfirm(command string) bool {
	fmt.Fprintf(os.Stderr, "\033[33mRun:\033[0m %s \033[2m[Y/n]\033[0m ", command)
	reader := t.stdinReader
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return false // fail closed on read error
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

// extractFirstLine returns the first non-empty line of output, trimmed and
// truncated to maxLen characters.
func extractFirstLine(output string, maxLen int) string {
	firstLine := strings.SplitN(output, "\n", 2)[0]
	firstLine = strings.TrimSpace(firstLine)
	if len(firstLine) > maxLen {
		firstLine = firstLine[:maxLen]
	}
	return firstLine
}

// formatCommandFeedback returns a formatted feedback string for display after
// a command completes. It includes a status icon and optional output preview.
func formatCommandFeedback(output string, exitCode int, timedOut bool) string {
	firstLine := extractFirstLine(output, 60)

	if timedOut {
		return "\033[33m⏱\033[0m timed out"
	}
	if exitCode == 0 {
		if firstLine != "" {
			return fmt.Sprintf("\033[32m✓\033[0m %s", firstLine)
		}
		return "\033[32m✓\033[0m"
	}
	// Non-zero exit
	if firstLine != "" {
		return fmt.Sprintf("\033[31m✗\033[0m exit %d: %s", exitCode, firstLine)
	}
	return fmt.Sprintf("\033[31m✗\033[0m exit %d", exitCode)
}
