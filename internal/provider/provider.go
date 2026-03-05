package provider

import (
	"context"
	"encoding/json"
)

// EventType enumerates the kinds of streaming events.
type EventType int

const (
	EventTextDelta EventType = iota
	EventToolCallStart
	EventToolCallDelta
	EventToolCallEnd
	EventUsage
	EventDone
	EventError
)

// Provider streams LLM responses.
type Provider interface {
	Stream(ctx context.Context, req *Request) (<-chan Event, error)
	Name() string
}

// Request is the unified request format for all providers.
type Request struct {
	Model     string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
	System    string
}

// Message represents a conversation message.
type Message struct {
	Role    Role
	Content []ContentBlock
}

// Role is the message author.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentBlock is a polymorphic content element within a message.
type ContentBlock struct {
	Type       string      // "text", "tool_use", "tool_result"
	Text       string      // for Type == "text"
	ToolCall   *ToolCall   // for Type == "tool_use"
	ToolResult *ToolResult // for Type == "tool_result"
}

// ToolCall represents the LLM requesting a tool invocation.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the response to a tool call.
type ToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// ToolDef describes a tool the LLM can invoke.
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Event is a single streaming event from the provider.
type Event struct {
	Type     EventType
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Error    error
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheCreate  int
}
