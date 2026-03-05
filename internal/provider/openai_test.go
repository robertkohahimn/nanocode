package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIStreamTextOnly(t *testing.T) {
	sseData := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"index\":0}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"index\":0}]}\n\ndata: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"stop\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	o := NewOpenAI("test-key", server.URL)
	events, err := o.Stream(context.Background(), &Request{
		Model:     "gpt-4o",
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
			t.Fatalf("error: %v", ev.Error)
		}
	}

	if text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", text)
	}
	if !gotDone {
		t.Error("expected Done event")
	}
}

func TestOpenAIStreamToolCall(t *testing.T) {
	sseData := "data: {\"choices\":[{\"delta\":{\"content\":\"Reading file.\"},\"index\":0}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_abc\",\"function\":{\"name\":\"read\",\"arguments\":\"\"}}]},\"index\":0}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"file_\"}}]},\"index\":0}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"path\\\":\\\"/tmp/test\\\"}\"}}]},\"index\":0}]}\n\ndata: {\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	o := NewOpenAI("test-key", server.URL)
	events, err := o.Stream(context.Background(), &Request{
		Model:     "gpt-4o",
		Messages:  []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "read /tmp/test"}}}},
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

	if text != "Reading file." {
		t.Errorf("expected text, got %q", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "read" {
		t.Errorf("expected 'read', got %q", toolCalls[0].Name)
	}
}

func TestOpenAIStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer server.Close()

	o := NewOpenAI("bad", server.URL)
	_, err := o.Stream(context.Background(), &Request{
		Model:     "gpt-4o",
		Messages:  []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
		MaxTokens: 1024,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAIName(t *testing.T) {
	o := NewOpenAI("key", "")
	if o.Name() != "openai" {
		t.Errorf("expected 'openai', got %q", o.Name())
	}
}
