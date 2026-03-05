package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicStreamTextOnly(t *testing.T) {
	sseData := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":25}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing api key")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing version header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	a := NewAnthropic("test-key", server.URL)
	events, err := a.Stream(context.Background(), &Request{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var text string
	var gotDone bool
	for ev := range events {
		switch ev.Type {
		case EventTextDelta:
			text += ev.Text
		case EventDone:
			gotDone = true
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}

	if text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", text)
	}
	if !gotDone {
		t.Error("expected Done event")
	}
}

func TestAnthropicStreamToolCall(t *testing.T) {
	sseData := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":50}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Let me read that.\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"read\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"file_\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"path\\\":\\\"/tmp/test.go\\\"}\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":30}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	a := NewAnthropic("test-key", server.URL)
	events, err := a.Stream(context.Background(), &Request{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "read /tmp/test.go"}}}},
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	var toolCalls []*ToolCall
	for ev := range events {
		switch ev.Type {
		case EventTextDelta:
			text += ev.Text
		case EventToolCallEnd:
			toolCalls = append(toolCalls, ev.ToolCall)
		case EventError:
			t.Fatalf("error: %v", ev.Error)
		}
	}

	if text != "Let me read that." {
		t.Errorf("expected text, got %q", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "read" {
		t.Errorf("expected tool name 'read', got %q", toolCalls[0].Name)
	}
	if toolCalls[0].ID != "toolu_01" {
		t.Errorf("expected tool ID 'toolu_01', got %q", toolCalls[0].ID)
	}
}

func TestAnthropicStreamError401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	a := NewAnthropic("bad-key", server.URL)
	_, err := a.Stream(context.Background(), &Request{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
		MaxTokens: 1024,
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestAnthropicName(t *testing.T) {
	a := NewAnthropic("key", "")
	if a.Name() != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", a.Name())
	}
}
