package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundedBuffer_Basic(t *testing.T) {
	b := newBoundedBuffer(16)
	b.write([]byte("hello"))
	if got := b.String(); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestBoundedBuffer_Wrap(t *testing.T) {
	b := newBoundedBuffer(8)
	b.write([]byte("12345678")) // fills exactly
	b.write([]byte("AB"))       // wraps: overwrites pos 0,1

	got := b.String()
	// Should contain the last 8 bytes: "345678AB"
	if got != "345678AB" {
		t.Errorf("got %q, want %q", got, "345678AB")
	}
}

func TestBoundedBuffer_Overflow(t *testing.T) {
	b := newBoundedBuffer(4)
	b.write([]byte("abcdefgh")) // 8 bytes into 4-byte buffer

	got := b.String()
	if got != "efgh" {
		t.Errorf("got %q, want %q", got, "efgh")
	}
}

func TestMCPTool_Name(t *testing.T) {
	info := ToolInfo{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	mt := NewMCPTool(info, nil, "fs_")
	if got := mt.Name(); got != "fs_read_file" {
		t.Errorf("got %q, want %q", got, "fs_read_file")
	}
}

func TestMCPTool_Definition(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	info := ToolInfo{
		Name:        "write_file",
		Description: "Write a file",
		InputSchema: schema,
	}
	mt := NewMCPTool(info, nil, "fs_")
	def := mt.Definition()

	if def.Name != "fs_write_file" {
		t.Errorf("name: got %q, want %q", def.Name, "fs_write_file")
	}
	if def.Description != "Write a file" {
		t.Errorf("description: got %q", def.Description)
	}
	if string(def.InputSchema) != string(schema) {
		t.Errorf("schema mismatch")
	}
}

func TestMCPTool_Execute(t *testing.T) {
	caller := func(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
		if method != "tools/call" {
			t.Errorf("expected tools/call, got %s", method)
		}
		cp, ok := params.(CallToolParams)
		if !ok {
			t.Fatal("params not CallToolParams")
		}
		// Verify original name is used (not prefixed)
		if cp.Name != "echo" {
			t.Errorf("expected original name 'echo', got %q", cp.Name)
		}

		result := CallToolResult{
			Content: []ContentItem{
				{Type: "text", Text: "hello world"},
			},
		}
		return json.Marshal(result)
	}

	info := ToolInfo{Name: "echo", Description: "Echo input", InputSchema: json.RawMessage(`{}`)}
	mt := NewMCPTool(info, caller, "test_")

	got, err := mt.Execute(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestMCPTool_Execute_Error(t *testing.T) {
	caller := func(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
		result := CallToolResult{
			Content: []ContentItem{{Type: "text", Text: "file not found"}},
			IsError: true,
		}
		return json.Marshal(result)
	}

	info := ToolInfo{Name: "read", Description: "Read", InputSchema: json.RawMessage(`{}`)}
	mt := NewMCPTool(info, caller, "")

	_, err := mt.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("error should contain 'file not found': %v", err)
	}
}

func TestMCPTool_Execute_MultiContent(t *testing.T) {
	caller := func(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
		result := CallToolResult{
			Content: []ContentItem{
				{Type: "text", Text: "line1"},
				{Type: "image", Text: ""},      // non-text, skip
				{Type: "text", Text: "line2"},
			},
		}
		return json.Marshal(result)
	}

	info := ToolInfo{Name: "multi", Description: "Multi", InputSchema: json.RawMessage(`{}`)}
	mt := NewMCPTool(info, caller, "")

	got, err := mt.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "line1\nline2" {
		t.Errorf("got %q, want %q", got, "line1\nline2")
	}
}

func TestMakeMCPTools(t *testing.T) {
	infos := []ToolInfo{
		{Name: "a", Description: "A"},
		{Name: "b", Description: "B"},
	}
	tools := MakeMCPTools(infos, nil, "srv_")
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	if tools[0].Name() != "srv_a" {
		t.Errorf("tools[0] name: got %q", tools[0].Name())
	}
	if tools[1].Name() != "srv_b" {
		t.Errorf("tools[1] name: got %q", tools[1].Name())
	}
}

func TestJSONRPCNotification_NoID(t *testing.T) {
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, err := json.Marshal(notif)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"id"`) {
		t.Errorf("notification should not contain 'id' field: %s", s)
	}
}

func TestJSONRPCRequest_HasID(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      0,
		Method:  "initialize",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"id"`) {
		t.Errorf("request should contain 'id' field: %s", s)
	}
}

func TestStdioClient_Echo(t *testing.T) {
	// Use a simple echo-back script to test the JSON-RPC flow.
	// The script reads JSON from stdin and responds with a matching response.
	script := `
import sys, json
while True:
    line = sys.stdin.readline()
    if not line:
        break
    try:
        req = json.loads(line)
    except:
        continue
    if 'id' not in req:
        continue
    if req['method'] == 'initialize':
        resp = {"jsonrpc":"2.0","id":req['id'],"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"test","version":"0.1.0"}}}
    elif req['method'] == 'tools/list':
        resp = {"jsonrpc":"2.0","id":req['id'],"result":{"tools":[{"name":"echo","description":"Echo tool","inputSchema":{"type":"object"}}]}}
    elif req['method'] == 'tools/call':
        text = json.dumps(req['params']['arguments'])
        resp = {"jsonrpc":"2.0","id":req['id'],"result":{"content":[{"type":"text","text":text}],"isError":False}}
    else:
        resp = {"jsonrpc":"2.0","id":req['id'],"error":{"code":-1,"message":"unknown"}}
    sys.stdout.write(json.dumps(resp) + '\n')
    sys.stdout.flush()
`
	client, err := NewStdioClient("python3", []string{"-c", script}, nil)
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Initialize
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// List tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	// Execute tool via adapter
	adapters := client.Tools("test_", tools)
	if len(adapters) != 1 {
		t.Fatalf("expected 1 adapter, got %d", len(adapters))
	}
	if adapters[0].Name() != "test_echo" {
		t.Errorf("adapter name: got %q", adapters[0].Name())
	}

	result, err := adapters[0].Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "hi") {
		t.Errorf("result should contain 'hi': %q", result)
	}
}

func TestStdioClient_Pagination(t *testing.T) {
	// MCP server that returns tools across two pages
	script := `
import sys, json
while True:
    line = sys.stdin.readline()
    if not line:
        break
    try:
        req = json.loads(line)
    except:
        continue
    if 'id' not in req:
        continue
    if req['method'] == 'initialize':
        resp = {"jsonrpc":"2.0","id":req['id'],"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"test","version":"0.1.0"}}}
    elif req['method'] == 'tools/list':
        cursor = (req.get('params') or {}).get('cursor', '')
        if cursor == '':
            resp = {"jsonrpc":"2.0","id":req['id'],"result":{"tools":[{"name":"a","description":"A","inputSchema":{"type":"object"}}],"nextCursor":"page2"}}
        else:
            resp = {"jsonrpc":"2.0","id":req['id'],"result":{"tools":[{"name":"b","description":"B","inputSchema":{"type":"object"}}]}}
    else:
        resp = {"jsonrpc":"2.0","id":req['id'],"error":{"code":-1,"message":"unknown"}}
    sys.stdout.write(json.dumps(resp) + '\n')
    sys.stdout.flush()
`
	client, err := NewStdioClient("python3", []string{"-c", script}, nil)
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools across pages, got %d", len(tools))
	}
	if tools[0].Name != "a" || tools[1].Name != "b" {
		t.Errorf("unexpected tool names: %s, %s", tools[0].Name, tools[1].Name)
	}
}
