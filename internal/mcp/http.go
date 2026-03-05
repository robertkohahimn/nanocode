package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/robertkohahimn/nanocode/internal/tool"
)

// HTTPClient communicates with an MCP server over HTTP POST (streamable-http transport).
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	sessionID  string // from Mcp-Session-Id response header
	mu         sync.Mutex
	nextID     int
}

// NewHTTPClient creates an MCP client for the streamable-http transport.
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
		nextID:     1,
	}
}

// Initialize performs the MCP handshake over HTTP.
func (c *HTTPClient) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    map[string]string{},
		ClientInfo:      ClientInfo{Name: "nanocode", Version: "0.1.0"},
	}

	if _, err := c.Call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp http initialize: %w", err)
	}

	// Send initialized notification
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp http notifications/initialized: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp http notifications/initialized: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListTools discovers all tools, handling cursor pagination.
func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var allTools []ToolInfo
	var cursor string

	for {
		var params interface{}
		if cursor != "" {
			params = ListToolsParams{Cursor: cursor}
		}

		raw, err := c.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("mcp http tools/list: %w", err)
		}

		var result ListToolsResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("mcp http: parsing tools/list: %w", err)
		}

		allTools = append(allTools, result.Tools...)

		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return allTools, nil
}

// Call sends a JSON-RPC request over HTTP POST and returns the result.
func (c *HTTPClient) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("mcp http: marshal %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp http %s: %w", method, err)
	}
	defer resp.Body.Close()

	// Track session ID from response
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("mcp http %s: status %d: %s", method, resp.StatusCode, string(errBody))
	}

	ct := resp.Header.Get("Content-Type")

	// SSE response: read events until we get the final result
	if strings.HasPrefix(ct, "text/event-stream") {
		return c.readSSEResult(resp.Body, id)
	}

	// JSON response: parse directly
	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("mcp http: decode %s response: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}
	return rpcResp.Result, nil
}

// readSSEResult reads SSE events and extracts the JSON-RPC result.
func (c *HTTPClient) readSSEResult(body io.Reader, expectedID int) (json.RawMessage, error) {
	reader := newSimpleSSEReader(body)
	for {
		event, err := reader.Next()
		if err != nil {
			return nil, fmt.Errorf("mcp http: reading SSE: %w", err)
		}
		if event == nil {
			return nil, fmt.Errorf("mcp http: SSE stream ended without result")
		}

		// Try to parse as JSON-RPC response
		if event.eventType == "message" || event.eventType == "" {
			var resp JSONRPCResponse
			if err := json.Unmarshal([]byte(event.data), &resp); err != nil {
				continue // skip non-JSON events
			}
			if resp.ID == expectedID {
				if resp.Error != nil {
					return nil, resp.Error
				}
				return resp.Result, nil
			}
		}
	}
}

// Tools returns tool.Tool adapters for all discovered MCP tools.
func (c *HTTPClient) Tools(prefix string, tools []ToolInfo) []tool.Tool {
	return MakeMCPTools(tools, c.Call, prefix)
}

func (c *HTTPClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	c.mu.Lock()
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()
}

// simpleSSEReader is a minimal SSE parser for the HTTP transport.
type simpleSSEReader struct {
	dec *json.Decoder // reuse buffered reader under the hood
	raw io.Reader
	buf []byte
}

type sseEvent struct {
	eventType string
	data      string
}

func newSimpleSSEReader(r io.Reader) *simpleSSEReader {
	return &simpleSSEReader{raw: r, buf: make([]byte, 0, 4096)}
}

// Next returns the next SSE event, or (nil, io.EOF) at stream end.
func (s *simpleSSEReader) Next() (*sseEvent, error) {
	// Read the body line by line
	var eventType string
	var dataLines []string
	scanner := &lineScanner{reader: s.raw, buf: s.buf}

	for {
		line, err := scanner.readLine()
		if err != nil {
			if err == io.EOF && len(dataLines) > 0 {
				return &sseEvent{eventType: eventType, data: strings.Join(dataLines, "\n")}, nil
			}
			return nil, err
		}

		if line == "" {
			if len(dataLines) == 0 {
				continue
			}
			s.buf = scanner.buf // preserve buffer state
			return &sseEvent{eventType: eventType, data: strings.Join(dataLines, "\n")}, nil
		}

		if strings.HasPrefix(line, ":") {
			continue // comment
		}

		if after, ok := strings.CutPrefix(line, "event:"); ok {
			eventType = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimPrefix(after, " "))
		}
	}
}

// lineScanner reads lines from a reader without requiring bufio.Scanner.
type lineScanner struct {
	reader io.Reader
	buf    []byte
	pos    int
}

func (ls *lineScanner) readLine() (string, error) {
	var savedErr error
	for {
		// Check for newline in existing buffer
		for i := ls.pos; i < len(ls.buf); i++ {
			if ls.buf[i] == '\n' {
				line := string(ls.buf[ls.pos:i])
				line = strings.TrimRight(line, "\r")
				ls.pos = i + 1
				return line, nil
			}
		}

		// No newline found. If we have a saved error, return remaining data.
		if savedErr != nil {
			if ls.pos < len(ls.buf) {
				line := string(ls.buf[ls.pos:])
				ls.buf = ls.buf[:0]
				ls.pos = 0
				return strings.TrimRight(line, "\r"), savedErr
			}
			return "", savedErr
		}

		// Compact buffer
		if ls.pos > 0 {
			remaining := len(ls.buf) - ls.pos
			copy(ls.buf[:remaining], ls.buf[ls.pos:])
			ls.buf = ls.buf[:remaining]
			ls.pos = 0
		}

		// Read more data
		tmp := make([]byte, 4096)
		n, err := ls.reader.Read(tmp)
		if n > 0 {
			ls.buf = append(ls.buf, tmp[:n]...)
		}
		if err != nil {
			savedErr = err
		}
	}
}
