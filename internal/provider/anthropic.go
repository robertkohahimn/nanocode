package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Anthropic implements the Provider interface for the Anthropic Messages API.
type Anthropic struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropic creates a new Anthropic provider.
func NewAnthropic(apiKey, baseURL string) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Anthropic{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 10 * time.Minute, // overall timeout prevents indefinite hangs on slow responses
			Transport: &http.Transport{
				ResponseHeaderTimeout: 30 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				DialContext: (&net.Dialer{
					Timeout: 10 * time.Second,
				}).DialContext,
			},
		},
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) Stream(ctx context.Context, req *Request) (<-chan Event, error) {
	body, err := a.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan Event, 32)
	go a.streamEvents(resp.Body, ch)
	return ch, nil
}

type toolCallBuilder struct {
	id   string
	name string
	buf  strings.Builder
}

func (a *Anthropic) streamEvents(body io.ReadCloser, ch chan<- Event) {
	defer close(ch)
	defer body.Close()

	reader := NewSSEReader(body)
	var toolBuilders []*toolCallBuilder

	for {
		sse, err := reader.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			ch <- Event{Type: EventError, Error: fmt.Errorf("reading SSE: %w", err)}
			return
		}

		switch sse.Type {
		case "message_start":
			var msg struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(sse.Data), &msg); err != nil {
				ch <- Event{Type: EventError, Error: fmt.Errorf("parsing message_start: %w", err)}
				return
			}
			ch <- Event{Type: EventUsage, Usage: &Usage{InputTokens: msg.Message.Usage.InputTokens}}

		case "content_block_start":
			const maxToolBuilders = 256

			var block struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(sse.Data), &block); err != nil {
				ch <- Event{Type: EventError, Error: fmt.Errorf("parsing content_block_start: %w", err)}
				return
			}
			if block.Index < 0 {
				ch <- Event{Type: EventError, Error: fmt.Errorf("invalid negative index %d in content_block_start", block.Index)}
				return
			}
			if block.Index > maxToolBuilders {
				ch <- Event{Type: EventError, Error: fmt.Errorf("content_block index %d exceeds maximum %d", block.Index, maxToolBuilders)}
				return
			}

			for len(toolBuilders) <= block.Index {
				toolBuilders = append(toolBuilders, nil)
			}

			if block.ContentBlock.Type == "tool_use" {
				toolBuilders[block.Index] = &toolCallBuilder{
					id:   block.ContentBlock.ID,
					name: block.ContentBlock.Name,
				}
				ch <- Event{
					Type:     EventToolCallStart,
					ToolCall: &ToolCall{ID: block.ContentBlock.ID, Name: block.ContentBlock.Name},
				}
			}

		case "content_block_delta":
			var delta struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(sse.Data), &delta); err != nil {
				ch <- Event{Type: EventError, Error: fmt.Errorf("parsing content_block_delta: %w", err)}
				return
			}
			if delta.Index < 0 {
				ch <- Event{Type: EventError, Error: fmt.Errorf("invalid negative index %d in content_block_delta", delta.Index)}
				return
			}

			if delta.Delta.Type == "text_delta" {
				ch <- Event{Type: EventTextDelta, Text: delta.Delta.Text}
			} else if delta.Delta.Type == "input_json_delta" {
				if delta.Index < len(toolBuilders) && toolBuilders[delta.Index] != nil {
					toolBuilders[delta.Index].buf.WriteString(delta.Delta.PartialJSON)
					ch <- Event{Type: EventToolCallDelta, Text: delta.Delta.PartialJSON}
				}
			}

		case "content_block_stop":
			var stop struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(sse.Data), &stop); err != nil {
				ch <- Event{Type: EventError, Error: fmt.Errorf("parsing content_block_stop: %w", err)}
				return
			}
			if stop.Index < 0 {
				ch <- Event{Type: EventError, Error: fmt.Errorf("invalid negative index %d in content_block_stop", stop.Index)}
				return
			}

			if stop.Index < len(toolBuilders) && toolBuilders[stop.Index] != nil {
				tb := toolBuilders[stop.Index]
				var input json.RawMessage
				if tb.buf.Len() > 0 {
					input = json.RawMessage(tb.buf.String())
				}
				ch <- Event{
					Type: EventToolCallEnd,
					ToolCall: &ToolCall{
						ID:    tb.id,
						Name:  tb.name,
						Input: input,
					},
				}
			}

		case "message_delta":
			var md struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(sse.Data), &md); err != nil {
				ch <- Event{Type: EventError, Error: fmt.Errorf("parsing message_delta: %w", err)}
				return
			}
			ch <- Event{Type: EventUsage, Usage: &Usage{OutputTokens: md.Usage.OutputTokens}}

		case "message_stop":
			ch <- Event{Type: EventDone}
			return

		case "error":
			var errResp struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(sse.Data), &errResp); err != nil {
				ch <- Event{Type: EventError, Error: fmt.Errorf("parsing error event: %w", err)}
				return
			}
			ch <- Event{Type: EventError, Error: fmt.Errorf("anthropic error: %s", errResp.Error.Message)}
			return
		}
	}
}

func (a *Anthropic) buildRequestBody(req *Request) ([]byte, error) {
	type apiMessage struct {
		Role    string        `json:"role"`
		Content []interface{} `json:"content"`
	}

	var messages []apiMessage
	for _, m := range req.Messages {
		am := apiMessage{Role: string(m.Role)}
		for _, cb := range m.Content {
			switch cb.Type {
			case "text":
				am.Content = append(am.Content, map[string]string{"type": "text", "text": cb.Text})
			case "tool_use":
				if cb.ToolCall == nil {
					continue
				}
				am.Content = append(am.Content, map[string]interface{}{
					"type":  "tool_use",
					"id":    cb.ToolCall.ID,
					"name":  cb.ToolCall.Name,
					"input": json.RawMessage(cb.ToolCall.Input),
				})
			case "tool_result":
				if cb.ToolResult == nil {
					continue
				}
				am.Content = append(am.Content, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": cb.ToolResult.ToolCallID,
					"content":     cb.ToolResult.Content,
					"is_error":    cb.ToolResult.IsError,
				})
			}
		}
		messages = append(messages, am)
	}

	type apiTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	var tools []apiTool
	for _, t := range req.Tools {
		tools = append(tools, apiTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}

	body := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages":   messages,
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	return json.Marshal(body)
}
