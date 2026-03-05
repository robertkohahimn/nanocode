package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// OpenAI implements the Provider interface for the OpenAI Chat Completions API.
type OpenAI struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAI creates a new OpenAI provider.
func NewOpenAI(apiKey, baseURL string) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAI{
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

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Stream(ctx context.Context, req *Request) (<-chan Event, error) {
	body, err := o.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("openai API error %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan Event, 32)
	go o.streamEvents(resp.Body, ch)
	return ch, nil
}

func (o *OpenAI) streamEvents(body io.ReadCloser, ch chan<- Event) {
	defer close(ch)
	defer body.Close()

	reader := NewSSEReader(body)
	toolAccumulators := make(map[int]*toolCallBuilder)

	for {
		sse, err := reader.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			ch <- Event{Type: EventError, Error: err}
			return
		}

		if sse.Data == "[DONE]" {
			ch <- Event{Type: EventDone}
			return
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   *string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(sse.Data), &chunk); err != nil {
			ch <- Event{Type: EventError, Error: fmt.Errorf("parsing chunk: %w", err)}
			return
		}

		if chunk.Usage != nil {
			ch <- Event{
				Type: EventUsage,
				Usage: &Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				},
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			ch <- Event{Type: EventTextDelta, Text: *choice.Delta.Content}
		}

		for _, tc := range choice.Delta.ToolCalls {
			if tc.ID != "" {
				toolAccumulators[tc.Index] = &toolCallBuilder{
					id:   tc.ID,
					name: tc.Function.Name,
				}
				ch <- Event{
					Type:     EventToolCallStart,
					ToolCall: &ToolCall{ID: tc.ID, Name: tc.Function.Name},
				}
			}
			if tc.Function.Arguments != "" {
				if acc, ok := toolAccumulators[tc.Index]; ok {
					acc.buf.WriteString(tc.Function.Arguments)
					ch <- Event{Type: EventToolCallDelta, Text: tc.Function.Arguments}
				}
			}
		}

		if choice.FinishReason != nil && len(toolAccumulators) > 0 {
			// Emit accumulated tool calls on any finish reason, in deterministic order
			indices := make([]int, 0, len(toolAccumulators))
			for idx := range toolAccumulators {
				indices = append(indices, idx)
			}
			sort.Ints(indices)
			for _, idx := range indices {
				acc := toolAccumulators[idx]
				ch <- Event{
					Type: EventToolCallEnd,
					ToolCall: &ToolCall{
						ID:    acc.id,
						Name:  acc.name,
						Input: json.RawMessage(acc.buf.String()),
					},
				}
			}
		}
	}
}

func (o *OpenAI) buildRequestBody(req *Request) ([]byte, error) {
	var messages []interface{}

	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}

	for _, m := range req.Messages {
		hasToolResults := false
		for _, cb := range m.Content {
			if cb.Type == "tool_result" && cb.ToolResult != nil {
				hasToolResults = true
				break
			}
		}

		if hasToolResults {
			for _, cb := range m.Content {
				if cb.Type == "tool_result" && cb.ToolResult != nil {
					messages = append(messages, map[string]string{
						"role":         "tool",
						"tool_call_id": cb.ToolResult.ToolCallID,
						"content":      cb.ToolResult.Content,
					})
				}
			}
			continue
		}

		hasToolCalls := false
		for _, cb := range m.Content {
			if cb.Type == "tool_use" && cb.ToolCall != nil {
				hasToolCalls = true
				break
			}
		}

		if hasToolCalls {
			var textParts []string
			var toolCalls []map[string]interface{}
			for _, cb := range m.Content {
				if cb.Type == "text" {
					textParts = append(textParts, cb.Text)
				} else if cb.Type == "tool_use" && cb.ToolCall != nil {
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   cb.ToolCall.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      cb.ToolCall.Name,
							"arguments": string(cb.ToolCall.Input),
						},
					})
				}
			}
			msg := map[string]interface{}{
				"role": string(m.Role),
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			if len(textParts) > 0 {
				msg["content"] = strings.Join(textParts, "\n")
			}
			messages = append(messages, msg)
			continue
		}

		var textParts []string
		for _, cb := range m.Content {
			if cb.Type == "text" {
				textParts = append(textParts, cb.Text)
			}
		}
		messages = append(messages, map[string]string{
			"role":    string(m.Role),
			"content": strings.Join(textParts, "\n"),
		})
	}

	type funcDef struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	type toolDef struct {
		Type     string  `json:"type"`
		Function funcDef `json:"function"`
	}
	var tools []toolDef
	for _, t := range req.Tools {
		tools = append(tools, toolDef{
			Type:     "function",
			Function: funcDef{Name: t.Name, Description: t.Description, Parameters: t.InputSchema},
		})
	}

	body := map[string]interface{}{
		"model":          req.Model,
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
		"messages":       messages,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	return json.Marshal(body)
}
