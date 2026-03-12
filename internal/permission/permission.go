package permission

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// matchGlob checks if text matches a simple glob pattern.
// Only * is supported as wildcard (matches any sequence of characters).
// Special case: "cmd *" also matches "cmd" (trailing space before * is optional).
func matchGlob(pattern, text string) bool {
	// Empty pattern only matches empty text
	if pattern == "" {
		return text == ""
	}

	// Split pattern by * wildcard
	parts := strings.Split(pattern, "*")

	// No wildcards = exact match
	if len(parts) == 1 {
		return pattern == text
	}

	// Special case: pattern ends with " *" (space-star) and text equals prefix without space.
	// This allows "ls *" to match both "ls" and "ls -la".
	if len(parts) == 2 && parts[1] == "" && strings.HasSuffix(parts[0], " ") {
		trimmedPrefix := strings.TrimSuffix(parts[0], " ")
		if text == trimmedPrefix {
			return true
		}
	}

	// Check prefix (before first *)
	if !strings.HasPrefix(text, parts[0]) {
		return false
	}
	text = text[len(parts[0]):]

	// Check middle parts
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(text, parts[i])
		if idx == -1 {
			return false
		}
		text = text[idx+len(parts[i]):]
	}

	// Check suffix (after last *)
	return strings.HasSuffix(text, parts[len(parts)-1])
}

// wrapperCommands can execute arbitrary commands as arguments and bypass validation.
var wrapperCommands = map[string]bool{
	"command": true,
	"env":     true,
	"xargs":   true,
	"find":    true,
	"nohup":   true,
	"timeout": true,
	"nice":    true,
	"strace":  true,
	"sudo":    true,
}

// canonicalize trims quotes and extracts the base name from a command string.
func canonicalize(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	return filepath.Base(raw)
}

// CheckResult contains the outcome of command validation.
type CheckResult struct {
	Allowed     bool   // passes allow/deny checks
	AutoApprove bool   // matches autoApprove pattern, skip confirmation
	Reason      string // explanation if not allowed
}

// Checker validates shell commands against allow/deny/autoApprove patterns.
// It parses the full bash AST to catch commands in pipes, subshells,
// command substitution, and process substitution.
type Checker struct {
	allow       map[string]bool // nil = allow all (exact match on base name)
	deny        []string        // glob patterns to block
	autoApprove []string        // glob patterns to auto-approve
}

// NewChecker creates a permission checker from allow/deny/autoApprove lists.
// - allow: exact command names permitted (nil = allow all)
// - deny: glob patterns to block
// - autoApprove: glob patterns to skip confirmation (implies allow)
func NewChecker(allow, deny, autoApprove []string) *Checker {
	c := &Checker{
		deny:        deny,
		autoApprove: autoApprove,
	}
	if len(allow) > 0 {
		c.allow = make(map[string]bool, len(allow))
		for _, cmd := range allow {
			if name := canonicalize(cmd); name != "" {
				c.allow[name] = true
			}
		}
	}
	return c
}

// Check parses a shell command string and validates every command
// against the allow/deny/autoApprove patterns. Returns CheckResult with:
// - Allowed: true if command passes all checks
// - AutoApprove: true if ALL commands match autoApprove patterns
// - Reason: explanation if not allowed
func (c *Checker) Check(command string) CheckResult {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: failed to parse command: %v", err)}
	}

	printer := syntax.NewPrinter()
	var result CheckResult
	result.Allowed = true
	result.AutoApprove = true // assume true, set false if any command doesn't match

	syntax.Walk(file, func(node syntax.Node) bool {
		if !result.Allowed {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Extract command name and normalize to base name
		var nameBuf strings.Builder
		printer.Print(&nameBuf, call.Args[0])
		name := nameBuf.String()
		nameBase := canonicalize(name)
		if nameBase == "" || nameBase == "." {
			result = CheckResult{Allowed: false, Reason: "blocked: empty command name"}
			return false
		}

		// Reject commands with variable expansion (can't validate statically)
		if containsExpansion(call.Args[0]) {
			result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: command uses variable expansion %q (cannot validate statically)", name)}
			return false
		}

		// Meta-command checks: eval, exec always blocked
		if nameBase == "eval" {
			result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: %q is not permitted (bypasses command validation)", nameBase)}
			return false
		}
		if nameBase == "exec" {
			result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: %q is not permitted (replaces shell process)", nameBase)}
			return false
		}

		// Wrapper commands that can execute arbitrary sub-commands
		if wrapperCommands[nameBase] {
			result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: %q is not permitted (can execute arbitrary commands)", nameBase)}
			return false
		}

		// bash -c / sh -c: check first argument
		if (nameBase == "bash" || nameBase == "sh") && len(call.Args) > 1 {
			var argBuf strings.Builder
			printer.Print(&argBuf, call.Args[1])
			if argBuf.String() == "-c" {
				result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: %q with -c is not permitted (arbitrary command execution)", nameBase)}
				return false
			}
		}

		// Build full command string for glob matching
		var fullCmdBuf strings.Builder
		printer.Print(&fullCmdBuf, call)
		fullCmd := fullCmdBuf.String()

		// Check deny patterns (glob matching on full command)
		for _, pattern := range c.deny {
			if matchGlob(pattern, fullCmd) || matchGlob(pattern, nameBase) {
				result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: %q matches deny pattern %q", fullCmd, pattern)}
				return false
			}
		}

		// Check autoApprove patterns (glob matching on full command)
		autoApproved := false
		for _, pattern := range c.autoApprove {
			if matchGlob(pattern, fullCmd) {
				autoApproved = true
				break
			}
		}

		// If command matches autoApprove, it implies allow (bypass allow list)
		if autoApproved {
			return true
		}

		// If we get here, command didn't match autoApprove
		result.AutoApprove = false

		// Check allow list (only if not auto-approved)
		if c.allow != nil && !c.allow[nameBase] {
			result = CheckResult{Allowed: false, Reason: fmt.Sprintf("blocked: %q is not in the allow list", nameBase)}
			return false
		}

		return true
	})

	return result
}

// containsExpansion checks if a word contains variable or command expansion
// that would make the command name unresolvable at parse time.
func containsExpansion(word *syntax.Word) bool {
	for _, part := range word.Parts {
		switch part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
			return true
		}
	}
	return false
}
