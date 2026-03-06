package permission

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

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

// Checker validates shell commands against allow/deny lists.
// It parses the full bash AST to catch commands in pipes, subshells,
// command substitution, and process substitution.
type Checker struct {
	allow map[string]bool // nil = allow all
	deny  map[string]bool // nil = deny none
}

// NewChecker creates a permission checker from allow/deny command lists.
// If allow is non-empty, only listed commands are permitted.
// If deny is non-empty, listed commands are blocked.
// Both can be set: command must be in allow AND not in deny.
func NewChecker(allow, deny []string) *Checker {
	c := &Checker{}
	if len(allow) > 0 {
		c.allow = make(map[string]bool, len(allow))
		for _, cmd := range allow {
			if name := canonicalize(cmd); name != "" {
				c.allow[name] = true
			}
		}
	}
	if len(deny) > 0 {
		c.deny = make(map[string]bool, len(deny))
		for _, cmd := range deny {
			if name := canonicalize(cmd); name != "" {
				c.deny[name] = true
			}
		}
	}
	return c
}

// Check parses a shell command string and validates every command
// against the allow/deny lists. Returns nil if all commands are
// permitted, or an error describing the first violation.
func (c *Checker) Check(command string) error {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return fmt.Errorf("blocked: failed to parse command: %w", err)
	}

	printer := syntax.NewPrinter()
	var walkErr error

	syntax.Walk(file, func(node syntax.Node) bool {
		if walkErr != nil {
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
			walkErr = fmt.Errorf("blocked: empty command name")
			return false
		}

		// Reject commands with variable expansion (can't validate statically)
		if containsExpansion(call.Args[0]) {
			walkErr = fmt.Errorf("blocked: command uses variable expansion %q (cannot validate statically)", name)
			return false
		}

		// Meta-command checks: eval, exec always blocked
		if nameBase == "eval" {
			walkErr = fmt.Errorf("blocked: %q is not permitted (bypasses command validation)", nameBase)
			return false
		}
		if nameBase == "exec" {
			walkErr = fmt.Errorf("blocked: %q is not permitted (replaces shell process)", nameBase)
			return false
		}

		// Wrapper commands that can execute arbitrary sub-commands
		if wrapperCommands[nameBase] {
			walkErr = fmt.Errorf("blocked: %q is not permitted (can execute arbitrary commands)", nameBase)
			return false
		}

		// bash -c / sh -c: check first argument
		if (nameBase == "bash" || nameBase == "sh") && len(call.Args) > 1 {
			var argBuf strings.Builder
			printer.Print(&argBuf, call.Args[1])
			if argBuf.String() == "-c" {
				walkErr = fmt.Errorf("blocked: %q with -c is not permitted (arbitrary command execution)", nameBase)
				return false
			}
		}

		// Check deny list
		if c.deny != nil && c.deny[nameBase] {
			walkErr = fmt.Errorf("blocked: %q is in the deny list", nameBase)
			return false
		}

		// Check allow list
		if c.allow != nil && !c.allow[nameBase] {
			walkErr = fmt.Errorf("blocked: %q is not in the allow list", nameBase)
			return false
		}

		return true
	})

	return walkErr
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
