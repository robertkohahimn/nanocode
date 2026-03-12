package engine

import (
	"fmt"
	"strconv"
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
