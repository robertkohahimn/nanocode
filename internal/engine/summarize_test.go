package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

func makeMsgs(n int) []provider.Message {
	msgs := make([]provider.Message, n)
	for i := range msgs {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		msgs[i] = provider.Message{
			Role:    role,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg%d", i)}},
		}
	}
	return msgs
}

func TestSummarizerBelowThreshold(t *testing.T) {
	s := NewSummarizer(nil, 30, 10)
	msgs := makeMsgs(20)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 20 {
		t.Errorf("expected 20 messages unchanged, got %d", len(result))
	}
}

func TestSummarizerDisabled(t *testing.T) {
	s := NewSummarizer(nil, 0, 10)
	msgs := makeMsgs(50)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 50 {
		t.Errorf("expected 50 messages unchanged when disabled, got %d", len(result))
	}
}

func TestSummarizerTriggersAboveThreshold(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Summary: files were edited."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, 30, 10)
	msgs := makeMsgs(35)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Should be: first message + summary message + 10 recent = 12
	if len(result) != 12 {
		t.Errorf("expected 12 messages, got %d", len(result))
	}

	// First message preserved
	if result[0].Content[0].Text != "msg0" {
		t.Errorf("expected first message preserved, got %q", result[0].Content[0].Text)
	}

	// Second message is summary
	if !strings.Contains(result[1].Content[0].Text, "<context-summary>") {
		t.Error("expected summary message with <context-summary> tags")
	}
	if !strings.Contains(result[1].Content[0].Text, "Summary: files were edited.") {
		t.Error("expected summary content from provider")
	}

	// Last message is the original last message
	if result[11].Content[0].Text != "msg34" {
		t.Errorf("expected last recent message to be msg34, got %q", result[11].Content[0].Text)
	}
}

func TestSummarizerFallbackOnError(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventError, Error: fmt.Errorf("API error")},
			},
		},
	}
	s := NewSummarizer(mp, 30, 10)
	msgs := makeMsgs(50) // >40 so windowMessages actually truncates
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal("should not return error, should fall back to windowing")
	}
	// Should fall back to windowMessages (maxContextMessages=40)
	if len(result) > maxContextMessages+1 { // +1 for tool_result pair adjustment
		t.Errorf("expected windowed result on provider failure, got %d messages", len(result))
	}
}

func TestSummarizerPreservesRecentMessages(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Summarized."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, 30, 10)
	msgs := makeMsgs(40)
	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Last 10 messages should be msgs[30..39]
	for i := 0; i < 10; i++ {
		expected := fmt.Sprintf("msg%d", 30+i)
		actual := result[2+i].Content[0].Text
		if actual != expected {
			t.Errorf("recent[%d]: expected %q, got %q", i, expected, actual)
		}
	}
}

func TestSummarizerWithExistingSummary(t *testing.T) {
	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Re-summarized."},
				{Type: provider.EventDone},
			},
		},
	}
	s := NewSummarizer(mp, 10, 5)

	msgs := makeMsgs(15)
	msgs[1] = provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{{
			Type: "text",
			Text: "<context-summary>\nPrevious summary content.\n</context-summary>",
		}},
	}

	result, err := s.MaybeSummarize(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Should still produce a valid result: first + summary + 5 recent = 7
	if len(result) != 7 {
		t.Errorf("expected 7 messages, got %d", len(result))
	}
}
