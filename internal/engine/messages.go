package engine

import (
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

// windowMessages prevents context overflow by keeping the first message
// (original user prompt) and the last maxN messages. It adjusts the cut
// point to avoid splitting tool_use/tool_result pairs, which would cause
// API errors from both Anthropic and OpenAI.
func windowMessages(msgs []provider.Message, maxN int) []provider.Message {
	if maxN <= 0 || len(msgs) <= maxN {
		return msgs
	}
	// Start index for the tail (after reserving slot 0 for the first message)
	startIdx := len(msgs) - (maxN - 1)

	// If we'd start on a tool_result message, back up to include
	// the preceding assistant message that contains the tool_use.
	if startIdx > 1 {
		msg := msgs[startIdx]
		hasToolResult := false
		for _, cb := range msg.Content {
			if cb.Type == "tool_result" {
				hasToolResult = true
				break
			}
		}
		if hasToolResult {
			startIdx--
		}
	}

	result := make([]provider.Message, 0, 1+len(msgs)-startIdx)
	result = append(result, msgs[0])
	result = append(result, msgs[startIdx:]...)
	return result
}

// collectResponse drains the event channel and builds the assistant message.
func collectResponse(events <-chan provider.Event, onEvent func(provider.Event)) (*provider.Message, error) {
	var textBuilder strings.Builder
	var toolCalls []*provider.ToolCall

	for ev := range events {
		if onEvent != nil {
			onEvent(ev)
		}

		switch ev.Type {
		case provider.EventTextDelta:
			textBuilder.WriteString(ev.Text)
		case provider.EventToolCallEnd:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, ev.ToolCall)
			}
		case provider.EventError:
			return nil, ev.Error
		}
	}

	msg := &provider.Message{Role: provider.RoleAssistant}
	if textBuilder.Len() > 0 {
		msg.Content = append(msg.Content, provider.ContentBlock{
			Type: "text",
			Text: textBuilder.String(),
		})
	}
	for _, tc := range toolCalls {
		msg.Content = append(msg.Content, provider.ContentBlock{
			Type:     "tool_use",
			ToolCall: tc,
		})
	}

	return msg, nil
}
