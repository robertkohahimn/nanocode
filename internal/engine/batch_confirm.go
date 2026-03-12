package engine

import (
	"fmt"
	"strings"
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

	return result, fmt.Errorf("unrecognized input: %q", input)
}
