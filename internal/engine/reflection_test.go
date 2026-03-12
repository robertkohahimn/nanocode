package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/store"
)

func TestEngineErrorReflectionParseError(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// First response: edit tool call with malformed JSON input
			{
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "edit",
					Input: json.RawMessage(`not valid json`),
				}},
				{Type: provider.EventDone},
			},
			// Second response: text only (ends loop)
			{
				{Type: provider.EventTextDelta, Text: "Sorry about that."},
				{Type: provider.EventDone},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil, false)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "edit something", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mp.requests) < 2 {
		t.Fatal("expected at least 2 provider requests")
	}

	// The second request's last user message should contain the reflection prompt
	secondReq := mp.requests[1]
	lastMsg := secondReq.Messages[len(secondReq.Messages)-1]
	foundReflection := false
	for _, cb := range lastMsg.Content {
		if cb.Type == "text" && strings.Contains(cb.Text, "<error-reflection>") {
			foundReflection = true
			break
		}
	}
	if !foundReflection {
		t.Error("expected error reflection prompt after parse error on edit tool call")
	}
}

func TestEngineErrorReflectionDoomLoop(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Build 6 edit tool calls to the same file in a single response.
	// The first 5 pass doom loop check (though they may fail at execution),
	// the 6th triggers the doom loop error with IsError: true.
	var toolCallEvents []provider.Event
	for i := 0; i < 6; i++ {
		toolCallEvents = append(toolCallEvents, provider.Event{
			Type: provider.EventToolCallEnd,
			ToolCall: &provider.ToolCall{
				ID:    fmt.Sprintf("tc%d", i+1),
				Name:  "edit",
				Input: json.RawMessage(`{"file_path":"/tmp/doom-target.txt","old_string":"x","new_string":"y"}`),
			},
		})
	}
	toolCallEvents = append(toolCallEvents, provider.Event{Type: provider.EventDone})

	mp := &mockProvider{
		responses: [][]provider.Event{
			toolCallEvents,
			// Final response: text only (ends loop)
			{
				{Type: provider.EventTextDelta, Text: "I'll stop now."},
				{Type: provider.EventDone},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil, false)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "keep editing", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mp.requests) < 2 {
		t.Fatal("expected at least 2 provider requests")
	}

	// The second request's last user message should contain:
	// 1. A tool_result with "Doom loop detected"
	// 2. A text block with the reflection prompt
	secondReq := mp.requests[1]
	lastMsg := secondReq.Messages[len(secondReq.Messages)-1]
	foundDoomLoop := false
	foundReflection := false
	for _, cb := range lastMsg.Content {
		if cb.Type == "tool_result" && cb.ToolResult != nil && strings.Contains(cb.ToolResult.Content, "Doom loop detected") {
			foundDoomLoop = true
		}
		if cb.Type == "text" && strings.Contains(cb.Text, "<error-reflection>") {
			foundReflection = true
		}
	}
	if !foundDoomLoop {
		t.Error("expected doom loop error in tool results")
	}
	if !foundReflection {
		t.Error("expected error reflection prompt after doom loop error")
	}
}

func TestEngineErrorReflection(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// First response: tool call to read a nonexistent file
			{
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "read",
					Input: json.RawMessage(`{"file_path":"/nonexistent/path/file.txt"}`),
				}},
				{Type: provider.EventDone},
			},
			// Second response: text only (ends loop)
			{
				{Type: provider.EventTextDelta, Text: "The file does not exist."},
				{Type: provider.EventDone},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil, false)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "read /nonexistent/path/file.txt", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mp.requests) < 2 {
		t.Fatal("expected at least 2 provider requests")
	}

	// The second request should contain the reflection prompt in the user message
	secondReq := mp.requests[1]
	lastMsg := secondReq.Messages[len(secondReq.Messages)-1]
	foundReflection := false
	for _, cb := range lastMsg.Content {
		if cb.Type == "text" && strings.Contains(cb.Text, "<error-reflection>") {
			foundReflection = true
			break
		}
	}
	if !foundReflection {
		t.Error("expected error reflection prompt in user message after tool failure")
	}
}

func TestEngineErrorReflectionDisabled(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// First response: tool call to read a nonexistent file
			{
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "read",
					Input: json.RawMessage(`{"file_path":"/nonexistent/path/file.txt"}`),
				}},
				{Type: provider.EventDone},
			},
			// Second response: text only (ends loop)
			{
				{Type: provider.EventTextDelta, Text: "The file does not exist."},
				{Type: provider.EventDone},
			},
		},
	}

	cfg := testConfig()
	cfg.DisableReflection = true

	eng := New(mp, st, cfg, nil, false)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "read /nonexistent/path/file.txt", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mp.requests) < 2 {
		t.Fatal("expected at least 2 provider requests")
	}

	// The second request should NOT contain the reflection prompt
	secondReq := mp.requests[1]
	lastMsg := secondReq.Messages[len(secondReq.Messages)-1]
	for _, cb := range lastMsg.Content {
		if cb.Type == "text" && strings.Contains(cb.Text, "<error-reflection>") {
			t.Error("reflection prompt should not appear when DisableReflection is true")
			return
		}
	}
}
