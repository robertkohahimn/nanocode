package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

const summaryPrompt = `Summarize this conversation segment concisely:
- Key decisions made
- Files read/modified
- Errors encountered and how resolved
- Current state of the task

Keep technical details. Omit pleasantries.`

const maxSummaryInput = 100 * 1024 // 100KB

// Summarizer compresses old conversation messages using the LLM provider.
type Summarizer struct {
	provider  provider.Provider
	model     string // model name to use for summarization requests
	threshold int    // message count to trigger summarization (0 = disabled)
	keepN     int    // number of recent messages to keep unsummarized
}

// NewSummarizer creates a Summarizer. If threshold is 0, summarization is disabled.
func NewSummarizer(p provider.Provider, model string, threshold, keepN int) *Summarizer {
	if keepN <= 0 {
		keepN = 10
	}
	return &Summarizer{provider: p, model: model, threshold: threshold, keepN: keepN}
}

// MaybeSummarize compresses messages if they exceed the threshold.
// On failure, falls back to windowMessages.
func (s *Summarizer) MaybeSummarize(ctx context.Context, messages []provider.Message) ([]provider.Message, error) {
	if s.threshold <= 0 || len(messages) < s.threshold {
		return messages, nil
	}

	first := messages[0]
	keepN := s.keepN
	if keepN >= len(messages)-1 {
		return messages, nil
	}
	cutPoint := len(messages) - keepN
	// If the cut point lands on a tool_result message, move it back by 1
	// to include the matching assistant tool_use message.
	if cutPoint > 1 {
		msg := messages[cutPoint]
		for _, cb := range msg.Content {
			if cb.Type == "tool_result" {
				cutPoint--
				break
			}
		}
	}
	middle := messages[1:cutPoint]
	recent := messages[cutPoint:]

	summary, err := s.generateSummary(ctx, middle)
	if err != nil {
		log.Printf("engine: summarization failed, falling back to windowing: %v", err)
		return windowMessages(messages, maxContextMessages), nil
	}

	result := make([]provider.Message, 0, 2+len(recent))
	result = append(result, first)
	result = append(result, provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("<context-summary>\n%s\n</context-summary>", summary),
		}},
	})
	result = append(result, recent...)
	return result, nil
}

// generateSummary calls the LLM to summarize a slice of messages.
func (s *Summarizer) generateSummary(ctx context.Context, messages []provider.Message) (string, error) {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] ", msg.Role))
		for _, cb := range msg.Content {
			switch cb.Type {
			case "text":
				sb.WriteString(cb.Text)
			case "tool_use":
				if cb.ToolCall != nil {
					sb.WriteString(fmt.Sprintf("<tool:%s>", cb.ToolCall.Name))
				}
			case "tool_result":
				if cb.ToolResult != nil {
					content := cb.ToolResult.Content
					if len(content) > 500 {
						cut := 500
						for cut > 0 && !utf8.RuneStart(content[cut]) {
							cut--
						}
						content = content[:cut] + "..."
					}
					sb.WriteString(content)
				}
			}
			sb.WriteByte('\n')
		}
	}

	input := sb.String()
	if len(input) > maxSummaryInput {
		// Truncate at a rune boundary to avoid splitting multi-byte UTF-8.
		cut := maxSummaryInput
		for cut > 0 && !utf8.RuneStart(input[cut]) {
			cut--
		}
		input = input[:cut] + "\n... (truncated)"
	}

	req := &provider.Request{
		Model: s.model,
		Messages: []provider.Message{
			{
				Role: provider.RoleUser,
				Content: []provider.ContentBlock{{
					Type: "text",
					Text: summaryPrompt + "\n\n---\n\n" + input,
				}},
			},
		},
		MaxTokens: 1024,
		System:    "You are a concise technical summarizer.",
	}

	events, err := s.provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("starting summary stream: %w", err)
	}

	var result strings.Builder
	for ev := range events {
		switch ev.Type {
		case provider.EventTextDelta:
			result.WriteString(ev.Text)
		case provider.EventError:
			return "", fmt.Errorf("summary stream error: %w", ev.Error)
		}
	}

	return result.String(), nil
}
