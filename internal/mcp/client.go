package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/tool"
)

// JSON-RPC 2.0 types

// JSONRPCRequest is a JSON-RPC 2.0 request with an ID (expects a response).
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCNotification is a JSON-RPC 2.0 notification (no ID, no response expected).
// Separate from JSONRPCRequest to avoid serializing "id":0.
type JSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is the error object in a JSON-RPC response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// MCP protocol types

// ToolInfo describes a tool exposed by an MCP server.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallToolParams is the params for a tools/call request.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// CallToolResult is the result of a tools/call response.
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError"`
}

// ContentItem is an element in MCP tool results.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ListToolsResult is the result of a tools/list response.
type ListToolsResult struct {
	Tools      []ToolInfo `json:"tools"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// ListToolsParams is the params for a tools/list request.
type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// InitializeParams is the params for the initialize request.
type InitializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    map[string]string `json:"capabilities"`
	ClientInfo      ClientInfo        `json:"clientInfo"`
}

// ClientInfo identifies the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Caller is a function that sends a JSON-RPC request and returns the result.
type Caller func(ctx context.Context, method string, params interface{}) (json.RawMessage, error)

// MCPTool adapts a single MCP tool to the tool.Tool interface.
type MCPTool struct {
	info   ToolInfo
	caller Caller
	prefix string // server name prefix, e.g. "myserver_"
}

// NewMCPTool creates an MCPTool adapter.
func NewMCPTool(info ToolInfo, caller Caller, prefix string) *MCPTool {
	return &MCPTool{info: info, caller: caller, prefix: prefix}
}

func (t *MCPTool) Name() string {
	return t.prefix + t.info.Name
}

func (t *MCPTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Name:        t.prefix + t.info.Name,
		Description: t.info.Description,
		InputSchema: t.info.InputSchema,
	}
}

func (t *MCPTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params := CallToolParams{
		Name:      t.info.Name, // use the original name, not prefixed
		Arguments: input,
	}
	raw, err := t.caller(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("mcp tools/call: %w", err)
	}

	var result CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp: parsing tools/call result: %w", err)
	}

	var parts []string
	for _, item := range result.Content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	text := strings.Join(parts, "\n")

	if result.IsError {
		if text == "" {
			text = "(no error details returned)"
		}
		return "", fmt.Errorf("mcp tool error: %s", text)
	}
	return text, nil
}

// MakeMCPTools creates tool.Tool adapters from a list of MCP tool infos.
func MakeMCPTools(tools []ToolInfo, caller Caller, prefix string) []tool.Tool {
	result := make([]tool.Tool, len(tools))
	for i, info := range tools {
		result[i] = NewMCPTool(info, caller, prefix)
	}
	return result
}
