package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/robertkohahimn/nanocode/internal/tool"
)

// StdioClient communicates with an MCP server over subprocess stdin/stdout.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	enc    *json.Encoder
	stderr *boundedBuffer

	mu       sync.Mutex // protects nextID, enc, and pending
	nextID   int
	pending  map[int]chan responseOrError // per-request response channels
	closeErr error                         // error from reader goroutine
}

type responseOrError struct {
	resp *JSONRPCResponse
	err  error
}

// NewStdioClient starts an MCP subprocess and prepares JSON-RPC communication.
func NewStdioClient(command string, args []string, env []string) (*StdioClient, error) {
	cmd := exec.Command(command, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		stdinPipe.Close()
		stdoutPipe.Close()
		return nil, fmt.Errorf("mcp stdio: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		stdoutPipe.Close()
		stderrPipe.Close()
		return nil, fmt.Errorf("mcp stdio: start %s: %w", command, err)
	}

	buf := newBoundedBuffer(64 * 1024)
	go buf.drainFrom(stderrPipe)

	c := &StdioClient{
		cmd:     cmd,
		stdin:   stdinPipe,
		enc:     json.NewEncoder(stdinPipe),
		stderr:  buf,
		nextID:  1,
		pending: make(map[int]chan responseOrError),
	}

	// Start background reader to demux responses
	dec := json.NewDecoder(stdoutPipe)
	go c.readLoop(dec)

	return c, nil
}

// readLoop continuously reads responses and routes them to pending requests.
func (c *StdioClient) readLoop(dec *json.Decoder) {
	for {
		var resp JSONRPCResponse
		if err := dec.Decode(&resp); err != nil {
			c.mu.Lock()
			c.closeErr = err
			// Signal all pending requests
			for id, ch := range c.pending {
				ch <- responseOrError{err: err}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}

		c.mu.Lock()
		if ch, ok := c.pending[resp.ID]; ok {
			ch <- responseOrError{resp: &resp}
			delete(c.pending, resp.ID)
		}
		// Ignore notifications (responses without matching ID)
		c.mu.Unlock()
	}
}

// Initialize performs the MCP handshake: initialize request + initialized notification.
func (c *StdioClient) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    map[string]string{},
		ClientInfo:      ClientInfo{Name: "nanocode", Version: "0.1.0"},
	}

	if _, err := c.Call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}

	// Send initialized notification (no ID, no response expected)
	c.mu.Lock()
	defer c.mu.Unlock()
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := c.enc.Encode(notif); err != nil {
		return fmt.Errorf("mcp notifications/initialized: %w", err)
	}
	return nil
}

// ListTools discovers all tools from the MCP server, handling cursor pagination.
func (c *StdioClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var allTools []ToolInfo
	var cursor string

	for {
		var params interface{}
		if cursor != "" {
			params = ListToolsParams{Cursor: cursor}
		}

		raw, err := c.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("mcp tools/list: %w", err)
		}

		var result ListToolsResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("mcp: parsing tools/list result: %w", err)
		}

		allTools = append(allTools, result.Tools...)

		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	return allTools, nil
}

// Call sends a JSON-RPC request and waits for the response.
func (c *StdioClient) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	// Create response channel and register it
	respCh := make(chan responseOrError, 1)

	c.mu.Lock()
	if c.closeErr != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp: connection closed: %w", c.closeErr)
	}

	id := c.nextID
	c.nextID++
	c.pending[id] = respCh

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.enc.Encode(req); err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp: sending %s: %w (stderr: %s)", method, err, c.stderr.String())
	}
	c.mu.Unlock()

	// Wait for response or context cancellation
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		// Don't kill the subprocess here - other calls may be in flight.
		// Process shutdown is handled by Close().
		return nil, ctx.Err()
	case result := <-respCh:
		if result.err != nil {
			return nil, fmt.Errorf("mcp: reading %s response: %w (stderr: %s)", method, result.err, c.stderr.String())
		}
		if result.resp.Error != nil {
			return nil, result.resp.Error
		}
		return result.resp.Result, nil
	}
}

// Tools returns tool.Tool adapters for all discovered MCP tools.
// Must call Initialize and ListTools first, or use ToolsFromServer.
func (c *StdioClient) Tools(prefix string, tools []ToolInfo) []tool.Tool {
	return MakeMCPTools(tools, c.Call, prefix)
}

// Close performs graceful shutdown: close stdin, wait with timeout, kill if needed.
func (c *StdioClient) Close() error {
	// Close stdin to signal EOF to child process
	c.stdin.Close()

	// Wait with 5-second timeout
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		// Force kill if graceful shutdown timed out
		_ = c.cmd.Process.Kill()
		<-done // reap the process
		return fmt.Errorf("mcp: process killed after 5s timeout")
	}
}

// boundedBuffer is a ring buffer capped at a fixed size, used to capture
// subprocess stderr without blocking.
type boundedBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	pos  int
	full bool
}

func newBoundedBuffer(cap int) *boundedBuffer {
	return &boundedBuffer{
		buf: make([]byte, cap),
		cap: cap,
	}
}

// drainFrom continuously reads from r into the ring buffer until EOF.
func (b *boundedBuffer) drainFrom(r io.Reader) {
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			b.write(tmp[:n])
		}
		if err != nil {
			return
		}
	}
}

func (b *boundedBuffer) write(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// If write length alone exceeds capacity, buffer will be full
	if len(data) >= b.cap {
		b.full = true
	}

	prevPos := b.pos
	for _, c := range data {
		b.buf[b.pos] = c
		b.pos = (b.pos + 1) % b.cap
	}

	// Buffer is full if we wrapped around (write crossed the capacity boundary)
	if !b.full && prevPos+len(data) >= b.cap {
		b.full = true
	}
}

// String returns the buffered stderr content.
func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.full {
		return string(b.buf[:b.pos])
	}
	// Ring buffer wrapped: return from pos to end, then start to pos
	result := make([]byte, b.cap)
	copy(result, b.buf[b.pos:])
	copy(result[b.cap-b.pos:], b.buf[:b.pos])
	return string(result)
}
