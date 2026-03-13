package engine

import (
	"strings"
	"testing"
)

func TestDefaultSystemPromptNotEmpty(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if prompt == "" {
		t.Fatal("DefaultSystemPrompt() returned empty string")
	}
}

func TestDefaultSystemPromptContainsKeyPolicies(t *testing.T) {
	prompt := DefaultSystemPrompt()

	required := []struct {
		phrase string
		desc   string
	}{
		// Identity
		{"Nanocode", "should identify as Nanocode"},
		{"coding assistant", "should describe role as coding assistant"},

		// Tool usage
		{"read a file before editing", "must require read before edit"},
		{"NEVER use bash commands", "must forbid bash for file ops"},
		{"`glob` tool", "must reference glob tool"},
		{"`grep` tool", "must reference grep tool"},
		{"`edit` tool", "must reference edit tool"},
		{"`write` tool", "must reference write tool"},
		{"`bash` tool", "must reference bash tool"},
		{"`subagent` tool", "must reference subagent tool"},

		// Git
		{"NEVER amend", "must forbid amending without permission"},
		{"NEVER force push", "must forbid force push"},
		{"NEVER skip pre-commit hooks", "must forbid skipping hooks"},
		{"NEW commits", "must prefer new commits"},

		// Verification
		{"run the relevant tests", "must require test verification"},
		{"Do not claim changes are complete without running verification", "must require verification"},
		{"do NOT retry the exact same approach", "must forbid retry loops"},

		// Error handling
		{"read the error message carefully", "must require error analysis"},
		{"ask for clarification", "must handle ambiguity"},

		// Communication
		{"concise and direct", "must require concise communication"},
		{"Report what you DID", "must require past-tense reporting"},

		// Safety
		{"NEVER run `rm -rf`", "must forbid rm -rf without confirmation"},
		{"secrets", "must address secret handling"},
		{"nanocode.md", "must reference project context file"},
	}

	for _, tc := range required {
		if !strings.Contains(prompt, tc.phrase) {
			t.Errorf("prompt missing required phrase %q (%s)", tc.phrase, tc.desc)
		}
	}
}

func TestDefaultSystemPromptLength(t *testing.T) {
	prompt := DefaultSystemPrompt()
	lines := strings.Split(prompt, "\n")
	if len(lines) < 200 {
		t.Errorf("prompt has %d lines, expected at least 200", len(lines))
	}
}
