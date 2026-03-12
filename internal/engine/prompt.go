package engine

import _ "embed"

//go:embed system_prompt.md
var embeddedSystemPrompt string

// DefaultSystemPrompt returns the comprehensive behavioral policies prompt
// embedded from system_prompt.md at compile time.
func DefaultSystemPrompt() string {
	return embeddedSystemPrompt
}
