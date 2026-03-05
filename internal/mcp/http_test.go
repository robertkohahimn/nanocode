package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClient_JSON(t *testing.T) {
	idCounter := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type: %s", ct)
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		// Check for notification (no response needed)
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusOK)
			return
		}

		idCounter++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "test-session-123")

		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"capabilities":   map[string]interface{}{},
				"serverInfo":     map[string]string{"name": "test", "version": "0.1.0"},
			}
		case "tools/list":
			result = ListToolsResult{
				Tools: []ToolInfo{
					{Name: "greet", Description: "Say hello", InputSchema: json.RawMessage(`{"type":"object"}`)},
				},
			}
		case "tools/call":
			result = CallToolResult{
				Content: []ContentItem{{Type: "text", Text: "hello!"}},
			}
		default:
			resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -1, Message: "unknown"}}
			json.NewEncoder(w).Encode(resp)
			return
		}

		raw, _ := json.Marshal(result)
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	ctx := t.Context()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Verify session ID was captured
	if client.sessionID != "test-session-123" {
		t.Errorf("session ID: got %q", client.sessionID)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "greet" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	adapters := client.Tools("http_", tools)
	result, err := adapters[0].Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello!" {
		t.Errorf("got %q, want %q", result, "hello!")
	}
}

func TestHTTPClient_SSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Respond with SSE for tools/call
		if req.Method == "tools/call" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			result := CallToolResult{
				Content: []ContentItem{{Type: "text", Text: "streamed result"}},
			}
			raw, _ := json.Marshal(result)
			resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
			respBytes, _ := json.Marshal(resp)

			fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(respBytes))
			return
		}

		// Default JSON response
		w.Header().Set("Content-Type", "application/json")
		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"capabilities":   map[string]interface{}{},
				"serverInfo":     map[string]string{"name": "test", "version": "0.1.0"},
			}
		case "tools/list":
			result = ListToolsResult{
				Tools: []ToolInfo{{Name: "stream_test", Description: "Test", InputSchema: json.RawMessage(`{}`)}},
			}
		}
		raw, _ := json.Marshal(result)
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	ctx := t.Context()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	adapters := client.Tools("", tools)
	result, err := adapters[0].Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "streamed result" {
		t.Errorf("got %q, want %q", result, "streamed result")
	}
}

func TestHTTPClient_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.Call(t.Context(), "initialize", nil)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestHTTPClient_SessionHeader(t *testing.T) {
	var receivedSessionID string
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		receivedSessionID = r.Header.Get("Mcp-Session-Id")

		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.Header().Set("Mcp-Session-Id", "sess-abc")
		}

		result, _ := json.Marshal(map[string]string{"ok": "true"})
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	ctx := t.Context()

	// First call: no session header sent
	client.Call(ctx, "initialize", nil)
	if receivedSessionID != "" {
		t.Errorf("first call should not have session ID, got %q", receivedSessionID)
	}

	// Second call: should send the session ID from first response
	client.Call(ctx, "tools/list", nil)
	if receivedSessionID != "sess-abc" {
		t.Errorf("second call should send session ID 'sess-abc', got %q", receivedSessionID)
	}
}
