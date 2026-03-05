package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nanocode/nanocode/internal/config"
	"github.com/nanocode/nanocode/internal/provider"
	"github.com/nanocode/nanocode/internal/store"
)

type mockProvider struct {
	responses [][]provider.Event
	callIdx   int
	requests  []*provider.Request
}

func (m *mockProvider) Stream(ctx context.Context, req *provider.Request) (<-chan provider.Event, error) {
	m.requests = append(m.requests, req)
	if m.callIdx >= len(m.responses) {
		return nil, fmt.Errorf("unexpected stream call %d", m.callIdx)
	}
	events := m.responses[m.callIdx]
	m.callIdx++

	ch := make(chan provider.Event, len(events))
	go func() {
		defer close(ch)
		for _, ev := range events {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (m *mockProvider) Name() string { return "mock" }

func testConfig() *config.Config {
	return &config.Config{
		Provider:  "mock",
		Model:     "test-model",
		MaxTokens: 1024,
		System:    "test system",
	}
}

func TestEngineTextOnly(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventTextDelta, Text: "Hello "},
				{Type: provider.EventTextDelta, Text: "world"},
				{Type: provider.EventDone},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil)
	ctx := context.Background()

	sessionID, _ := st.CreateSession(ctx, "/tmp")
	var output string
	err = eng.Run(ctx, sessionID, "hi", func(ev provider.Event) {
		if ev.Type == provider.EventTextDelta {
			output += ev.Text
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", output)
	}

	// Verify messages persisted
	msgs, _ := st.GetMessages(ctx, sessionID)
	if len(msgs) != 2 { // user + assistant
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestEngineToolCallThenText(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// First response: text + tool call
			{
				{Type: provider.EventTextDelta, Text: "Let me read that"},
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "read",
					Input: json.RawMessage(`{"file_path":"/dev/null"}`),
				}},
				{Type: provider.EventDone},
			},
			// Second response: text only (no more tools)
			{
				{Type: provider.EventTextDelta, Text: "Done"},
				{Type: provider.EventDone},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "read /dev/null", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mp.callIdx != 2 {
		t.Errorf("expected 2 provider calls, got %d", mp.callIdx)
	}

	// Second request should include the tool result
	if len(mp.requests) < 2 {
		t.Fatal("expected 2 requests")
	}
	msgs := mp.requests[1].Messages
	// Should have: user prompt, assistant (text+tool), user (tool result)
	if len(msgs) < 3 {
		t.Errorf("expected at least 3 messages in second request, got %d", len(msgs))
	}
}

func TestEngineProviderError(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			{
				{Type: provider.EventError, Error: fmt.Errorf("API down")},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "hi", func(ev provider.Event) {})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEngineContextCancellation(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	mp := &mockProvider{
		responses: [][]provider.Event{
			{{Type: provider.EventTextDelta, Text: "hi"}},
		},
	}

	eng := New(mp, st, testConfig(), nil)
	sessionID, _ := st.CreateSession(context.Background(), "/tmp")

	err = eng.Run(ctx, sessionID, "hi", func(ev provider.Event) {})
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestWindowMessages(t *testing.T) {
	// Create 10 messages
	msgs := make([]provider.Message, 10)
	for i := range msgs {
		msgs[i] = provider.Message{
			Role:    provider.RoleUser,
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg%d", i)}},
		}
	}

	// Window to 5: keep first + last 4
	windowed := windowMessages(msgs, 5)
	if len(windowed) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(windowed))
	}
	// First should be msg0
	if windowed[0].Content[0].Text != "msg0" {
		t.Errorf("first message should be msg0, got %q", windowed[0].Content[0].Text)
	}
	// Last should be msg9
	if windowed[4].Content[0].Text != "msg9" {
		t.Errorf("last message should be msg9, got %q", windowed[4].Content[0].Text)
	}

	// Under limit: no change
	small := msgs[:3]
	windowed = windowMessages(small, 5)
	if len(windowed) != 3 {
		t.Errorf("expected 3 messages, got %d", len(windowed))
	}
}
