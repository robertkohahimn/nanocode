package permission

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// wrapperCommands can execute arbitrary commands as arguments and bypass validation.
var wrapperCommands = map[string]bool{
	"bash":     true, // can execute scripts or commands
	"sh":       true, // can execute scripts or commands
	"builtin":  true, // can invoke shell builtins like eval
	"source":   true, // sources/executes script files
	"command":  true,
	"env":      true,
	"xargs":    true,
	"find":     true,
	"nohup":    true,
	"timeout":  true,
	"nice":     true,
	"strace":   true,
	"sudo":     true,
	"time":     true,
	"watch":    true,
	"parallel": true,
	"ssh":      true,
	"su":       true,
	"doas":     true,
	"runuser":  true,
	"pkexec":   true,
	"chroot":   true,
	"script":   true,
}

// canonicalize normalizes a command name by stripping leading backslashes,
// removing outer quotes, and extracting the base name.
func canonicalize(raw string) string {
	raw = strings.TrimSpace(raw)
	// Strip leading backslashes (e.g., \eval -> eval)
	raw = strings.TrimLeft(raw, `\`)
	raw = strings.Trim(raw, `"'`)
	return filepath.Base(raw)
}

// extractCommandName resolves the command name from an AST word by
// concatenating literal parts. This handles mixed quotes like "ev"'al'.
func extractCommandName(word *syntax.Word) string {
	var result strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			result.WriteString(p.Value)
		case *syntax.SglQuoted:
			result.WriteString(p.Value)
		case *syntax.DblQuoted:
			// Recurse into double-quoted parts
			for _, inner := range p.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					result.WriteString(lit.Value)
				}
			}
		}
	}
	return result.String()
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

	var walkErr error

	syntax.Walk(file, func(node syntax.Node) bool {
		if walkErr != nil {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Extract command name using AST to handle mixed quotes properly
		name := extractCommandName(call.Args[0])
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
		// This includes bash/sh which are blocked entirely rather than trying to
		// detect dangerous flags like -c, since they can execute scripts directly.
		if wrapperCommands[nameBase] {
			walkErr = fmt.Errorf("blocked: %q is not permitted (can execute arbitrary commands)", nameBase)
			return false
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
// Recursively checks inside quoted structures to catch nested expansions.
func containsExpansion(word *syntax.Word) bool {
	return containsExpansionParts(word.Parts)
}

// containsExpansionParts recursively checks word parts for expansions.
func containsExpansionParts(parts []syntax.WordPart) bool {
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
			return true
		case *syntax.DblQuoted:
			// Recurse into double-quoted parts to catch nested expansions
			if containsExpansionParts(p.Parts) {
				return true
			}
		}
	}
	return false
}
