package engine

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/permission"
	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/tool"
)

// parseSelection parses user input and returns a slice of booleans indicating
// which indices (0-based) are selected. count is the total number of commands.
func parseSelection(input string, count int) ([]bool, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	result := make([]bool, count)

	// Empty, "y", or "yes" = approve all
	if input == "" || input == "y" || input == "yes" {
		for i := range result {
			result[i] = true
		}
		return result, nil
	}

	// "n" or "no" = reject all
	if input == "n" || input == "no" {
		return result, nil // all false
	}

	// Parse comma/space-separated numbers
	// Replace commas with spaces for uniform splitting
	input = strings.ReplaceAll(input, ",", " ")
	parts := strings.Fields(input)

	for _, part := range parts {
		// Check for range (e.g., "1-3")
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range: %q", part)
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("invalid range: %q", part)
			}
			if start < 1 || end > count || start > end {
				return nil, fmt.Errorf("invalid range %d-%d for %d commands", start, end, count)
			}
			for i := start; i <= end; i++ {
				result[i-1] = true
			}
			continue
		}

		// Single number
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid selection: %q", part)
		}
		if n < 1 || n > count {
			return nil, fmt.Errorf("selection %d out of range (1-%d)", n, count)
		}
		result[n-1] = true
	}

	return result, nil
}

// batchDecision represents the user's decision for a single command.
type batchDecision struct {
	approved bool
	skipped  bool // true if user selected others but not this one
}

// pendingCommand represents a bash command awaiting confirmation.
type pendingCommand struct {
	toolCallID string
	command    string
}

// promptBatch displays a numbered list of commands and prompts for selection.
// Returns a map of toolCallID -> decision.
func promptBatch(commands []pendingCommand, reader *bufio.Reader, output io.Writer) (map[string]batchDecision, error) {
	// Display numbered list
	fmt.Fprintf(output, "\033[33mPending commands:\033[0m\n")
	for i, cmd := range commands {
		fmt.Fprintf(output, "  %d. %s\n", i+1, cmd.command)
	}
	fmt.Fprintf(output, "\nRun all? \033[2m[Y/n/1,3,4]\033[0m ")

	// Read user input
	line, err := reader.ReadString('\n')
	if err != nil {
		// On EOF or error, reject all
		result := make(map[string]batchDecision)
		for _, cmd := range commands {
			result[cmd.toolCallID] = batchDecision{approved: false, skipped: false}
		}
		return result, nil
	}

	// Parse selection
	selected, err := parseSelection(line, len(commands))
	if err != nil {
		// On parse error, ask again (for now, just return the error)
		return nil, fmt.Errorf("invalid selection: %w", err)
	}

	// Build decisions map
	result := make(map[string]batchDecision)
	anySelected := false
	for _, s := range selected {
		if s {
			anySelected = true
			break
		}
	}

	for i, cmd := range commands {
		if selected[i] {
			result[cmd.toolCallID] = batchDecision{approved: true, skipped: false}
		} else {
			// skipped = true only if user selected others (partial selection)
			result[cmd.toolCallID] = batchDecision{approved: false, skipped: anySelected}
		}
	}

	return result, nil
}

// collectBashConfirmations checks pending bash commands and prompts for batch confirmation.
// Returns nil if batch confirmation was not needed (0-1 commands needing confirmation).
func collectBashConfirmations(
	toolCalls []*provider.ToolCall,
	bashTool *tool.BashTool,
	permChecker *permission.Checker,
	reader *bufio.Reader,
	output io.Writer,
) error {
	// Collect bash commands that need confirmation
	var pending []pendingCommand
	for _, tc := range toolCalls {
		if tc.Name != "bash" {
			continue
		}

		// Parse the bash input to get the command
		in, err := tool.ParseInput[tool.BashInput](tc.Input)
		if err != nil {
			// Skip commands we can't parse - they'll fail at execution time
			continue
		}

		// Check if command needs confirmation using permission checker
		// If permChecker is nil, all commands need confirmation
		needsConfirm := true
		if permChecker != nil {
			// If Check returns nil, command is auto-approved (allowed by rules)
			// But we still need to ask for confirmation in batch mode
			// The permission checker just blocks denied commands
			if err := permChecker.Check(in.Command); err != nil {
				// Command is blocked by permission rules - skip it
				// It will fail when executed
				continue
			}
		}

		if needsConfirm {
			pending = append(pending, pendingCommand{
				toolCallID: tc.ID,
				command:    in.Command,
			})
		}
	}

	// If 0-1 commands need confirmation, let normal flow handle it
	if len(pending) <= 1 {
		return nil
	}

	// Prompt for batch confirmation
	decisions, err := promptBatch(pending, reader, output)
	if err != nil {
		return fmt.Errorf("batch confirmation: %w", err)
	}

	// Set overrides on bash tool
	for toolCallID, dec := range decisions {
		bashTool.SetConfirmOverride(toolCallID, dec.approved, dec.skipped)
	}

	return nil
}
