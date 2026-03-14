package engine

import (
	"fmt"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// CheckpointInjector injects periodic progress reminders into the conversation.
// Reminders escalate in urgency as iterations increase.
type CheckpointInjector struct {
	interval int // 0 = disabled
}

// NewCheckpointInjector creates a CheckpointInjector with the given interval.
// An interval of 0 disables checkpoint injection.
func NewCheckpointInjector(interval int) *CheckpointInjector {
	return &CheckpointInjector{interval: interval}
}

// MaybeInject returns a checkpoint content block and its urgency level if one
// should be injected at the given iteration. Returns nil, "" otherwise.
func (ci *CheckpointInjector) MaybeInject(iteration int) (*provider.ContentBlock, string) {
	if ci.interval <= 0 || iteration == 0 {
		return nil, ""
	}
	if iteration%ci.interval != 0 {
		return nil, ""
	}
	level, body := ci.escalation(iteration)
	text := fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>", body)
	return &provider.ContentBlock{Type: "text", Text: text}, level
}

// escalation returns the urgency level and message body for a given iteration.
func (ci *CheckpointInjector) escalation(iteration int) (string, string) {
	switch {
	case iteration >= 40:
		return "urgent", fmt.Sprintf(
			"Checkpoint (iteration %d of %d):\n"+
				"You have used most of your iteration budget.\n"+
				"Consider stopping and reporting partial progress.\n"+
				"If stuck, ask the user for help rather than continuing to iterate.",
			iteration, maxIterations)
	case iteration >= 25:
		return "warning", fmt.Sprintf(
			"Checkpoint (iteration %d of %d):\n"+
				"You're halfway through your iteration budget.\n"+
				"- Are you stuck? If so, try a different approach.\n"+
				"- Are you making progress? If so, what's remaining?",
			iteration, maxIterations)
	default:
		return "gentle", fmt.Sprintf(
			"Checkpoint (iteration %d of %d):\n"+
				"- What have you accomplished so far?\n"+
				"- What's remaining?\n"+
				"- Are you on track or stuck?\n\n"+
				"If stuck, consider: asking for help, trying a different approach, or simplifying the goal.",
			iteration, maxIterations)
	}
}
