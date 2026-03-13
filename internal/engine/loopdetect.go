package engine

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// LoopWarning describes a detected loop pattern.
type LoopWarning struct {
	Type   string // "oscillation", "repeated_edit", "repeated_error", "repeated_command", "edit_count"
	Detail string // Human-readable description
}

// LoopDetector tracks patterns that indicate the model is stuck.
type LoopDetector struct {
	editHashes   map[string][]string // file -> list of content hashes
	editCounts   map[string]int      // file -> total edit count
	errorHistory []string            // normalized recent errors
	cmdHistory   []string            // recent bash commands
	maxFileEdits int
	maxErrors    int
	maxCmds      int
}

// NewLoopDetector creates a detector with default thresholds.
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{
		editHashes:   make(map[string][]string),
		editCounts:   make(map[string]int),
		maxFileEdits: 5,
		maxErrors:    3,
		maxCmds:      3,
	}
}

const loopInterventionPrompt = `<loop-detected type="%s">
%s

Your current approach is NOT working. Before your next action:
1. What fundamental assumption is wrong?
2. What completely different approach could work?
3. Should you ask the user for help?

Do NOT retry the same approach. Try something fundamentally different or ask for help.
</loop-detected>`

// FormatWarning formats a LoopWarning into an intervention prompt string.
func FormatWarning(w *LoopWarning) string {
	return fmt.Sprintf(loopInterventionPrompt, w.Type, w.Detail)
}

// contentHash returns the first 16 hex chars of the SHA-256 hash of content.
func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8])
}

// CheckEdit checks for edit-related loop patterns on a file.
// Returns the most severe warning found, or nil.
func (ld *LoopDetector) CheckEdit(file, content string) *LoopWarning {
	hash := contentHash(content)
	ld.editHashes[file] = append(ld.editHashes[file], hash)
	ld.editCounts[file]++

	hashes := ld.editHashes[file]

	// Check edit count threshold first (most severe — hard block)
	if ld.editCounts[file] > ld.maxFileEdits {
		return &LoopWarning{
			Type:   "edit_count",
			Detail: fmt.Sprintf("File %s has been edited %d times (limit: %d). Stop and report to the user.", file, ld.editCounts[file], ld.maxFileEdits),
		}
	}

	// Check oscillation: A -> B -> A pattern (hash[n] == hash[n-2])
	if n := len(hashes); n >= 3 && hashes[n-1] == hashes[n-3] && hashes[n-1] != hashes[n-2] {
		return &LoopWarning{
			Type:   "oscillation",
			Detail: fmt.Sprintf("File %s is oscillating between two states (A->B->A pattern detected).", file),
		}
	}

	// Check repeated identical edits: same hash appears 2+ times
	hashCount := 0
	for _, h := range hashes {
		if h == hash {
			hashCount++
		}
	}
	if hashCount >= 2 {
		return &LoopWarning{
			Type:   "repeated_edit",
			Detail: fmt.Sprintf("File %s has been written with identical content %d times.", file, hashCount),
		}
	}

	return nil
}

// regNumbers matches numeric sequences for normalization.
var regNumbers = regexp.MustCompile(`[0-9]+`)

// regHexAddr matches hex addresses like 0x1a2b3c.
var regHexAddr = regexp.MustCompile(`0x[0-9a-fA-F]+`)

// regWhitespace collapses runs of whitespace.
var regWhitespace = regexp.MustCompile(`\s+`)

// normalizeError strips line numbers, hex addresses, lowercases, and collapses whitespace.
func normalizeError(s string) string {
	s = regHexAddr.ReplaceAllString(s, "")
	s = regNumbers.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = regWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// CheckError checks for repeated error patterns.
// Returns a warning if the same (normalized) error appears maxErrors times, or nil.
func (ld *LoopDetector) CheckError(errMsg string) *LoopWarning {
	norm := normalizeError(errMsg)
	ld.errorHistory = append(ld.errorHistory, norm)

	// Keep last 20
	if len(ld.errorHistory) > 20 {
		ld.errorHistory = ld.errorHistory[len(ld.errorHistory)-20:]
	}

	count := 0
	for _, e := range ld.errorHistory {
		if e == norm {
			count++
		}
	}
	if count >= ld.maxErrors {
		// Truncate detail for readability
		detail := errMsg
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
		return &LoopWarning{
			Type:   "repeated_error",
			Detail: fmt.Sprintf("The same error has occurred %d times: %s", count, detail),
		}
	}
	return nil
}

// CheckCommand checks for repeated command patterns.
// Returns a warning if the same command appears maxCmds times in the last 10, or nil.
func (ld *LoopDetector) CheckCommand(cmd string) *LoopWarning {
	normalized := strings.TrimSpace(cmd)
	ld.cmdHistory = append(ld.cmdHistory, normalized)

	// Keep last 20
	if len(ld.cmdHistory) > 20 {
		ld.cmdHistory = ld.cmdHistory[len(ld.cmdHistory)-20:]
	}

	// Check the last 10 commands for repeats
	start := len(ld.cmdHistory) - 10
	if start < 0 {
		start = 0
	}
	recent := ld.cmdHistory[start:]
	count := 0
	for _, c := range recent {
		if c == normalized {
			count++
		}
	}
	if count >= ld.maxCmds {
		return &LoopWarning{
			Type:   "repeated_command",
			Detail: fmt.Sprintf("Command has been run %d times recently without changes: %s", count, normalized),
		}
	}
	return nil
}
