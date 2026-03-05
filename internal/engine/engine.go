package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/robertkohahimn/nanocode/internal/config"
	"github.com/robertkohahimn/nanocode/internal/mcp"
	"github.com/robertkohahimn/nanocode/internal/permission"
	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/snapshot"
	"github.com/robertkohahimn/nanocode/internal/store"
	"github.com/robertkohahimn/nanocode/internal/tool"
)

const maxIterations = 50
const maxContextMessages = 40

const defaultSystem = `You are Nanocode, a coding assistant. You have access to tools for reading, writing, and editing files, running shell commands, searching codebases, and delegating sub-tasks.

When the user gives you a task:
1. Read relevant files to understand the codebase.
2. Plan your approach.
3. Make changes using the edit and write tools.
4. Verify your changes work by running tests or the build.
5. Report what you did.

Be precise. Make minimal changes. Explain your reasoning.`

// Engine is the core conversation loop.
type Engine struct {
	provider   provider.Provider
	tools      *ToolRegistry
	store      store.Store
	config     *config.Config
	mcpClients []io.Closer          // MCP subprocess handles
	snapMgr    *snapshot.Manager     // nil if no project dir
}

// New creates an Engine with the given dependencies.
// stdinReader is shared with the REPL loop to avoid conflicting buffered readers.
func New(p provider.Provider, s store.Store, cfg *config.Config, stdinReader *bufio.Reader) *Engine {
	bashTool := tool.NewBashTool(stdinReader)

	// Permission system: wire allow/deny lists into bash confirm hook
	if bashCfg, ok := cfg.Tools["bash"]; ok {
		if len(bashCfg.Allow) > 0 || len(bashCfg.Deny) > 0 {
			checker := permission.NewChecker(bashCfg.Allow, bashCfg.Deny)
			origConfirm := bashTool.ConfirmFunc
			bashTool.ConfirmFunc = func(cmd string) bool {
				if err := checker.Check(cmd); err != nil {
					fmt.Fprintf(os.Stderr, "\033[31mBlocked:\033[0m %s\n", err)
					return false
				}
				return origConfirm(cmd)
			}
		}
	}

	// Snapshot tracking
	baseDir := cfg.ProjectDir
	var snapMgr *snapshot.Manager
	var onChange func(string)
	if baseDir != "" {
		snapMgr = snapshot.New(baseDir, s)
		onChange = snapMgr.Track
	}

	writeTool := &tool.WriteTool{BaseDir: baseDir, OnChange: onChange}
	editTool := &tool.EditTool{BaseDir: baseDir, OnChange: onChange}

	// Collect built-in tools
	allTools := []tool.Tool{
		&tool.ReadTool{BaseDir: baseDir},
		writeTool, editTool,
		&tool.GlobTool{},
		&tool.GrepTool{BaseDir: baseDir},
		bashTool,
	}

	// MCP tools
	var mcpClients []io.Closer
	for name, serverCfg := range cfg.MCPServers {
		var mcpTools []tool.Tool
		switch serverCfg.Transport {
		case "stdio":
			client, err := mcp.NewStdioClient(serverCfg.Command, serverCfg.Args, serverCfg.Env)
			if err != nil {
				log.Printf("mcp: failed to start %s: %v", name, err)
				continue
			}
			if err := client.Initialize(context.Background()); err != nil {
				client.Close()
				log.Printf("mcp: failed to initialize %s: %v", name, err)
				continue
			}
			tools, err := client.ListTools(context.Background())
			if err != nil {
				client.Close()
				log.Printf("mcp: failed to list tools from %s: %v", name, err)
				continue
			}
			mcpTools = client.Tools(name+"_", tools)
			mcpClients = append(mcpClients, client)
		case "http":
			client := mcp.NewHTTPClient(serverCfg.URL)
			if err := client.Initialize(context.Background()); err != nil {
				log.Printf("mcp: failed to initialize %s: %v", name, err)
				continue
			}
			tools, err := client.ListTools(context.Background())
			if err != nil {
				log.Printf("mcp: failed to list tools from %s: %v", name, err)
				continue
			}
			mcpTools = client.Tools(name+"_", tools)
		default:
			log.Printf("mcp: unknown transport %q for server %s", serverCfg.Transport, name)
			continue
		}
		allTools = append(allTools, mcpTools...)
	}

	eng := &Engine{
		provider:   p,
		store:      s,
		config:     cfg,
		mcpClients: mcpClients,
		snapMgr:    snapMgr,
	}

	subagentTool := &tool.SubagentTool{Runner: eng}
	allTools = append(allTools, subagentTool)
	eng.tools = NewToolRegistry(allTools...)

	return eng
}

// Close shuts down MCP subprocesses. Must be called on exit.
func (e *Engine) Close() {
	for _, c := range e.mcpClients {
		c.Close()
	}
}

// persistMessage marshals content and appends it to the store.
// Errors are logged but not returned to avoid breaking the conversation loop.
func (e *Engine) persistMessage(ctx context.Context, sessionID string, role provider.Role, content interface{}) {
	if sessionID == "" {
		return
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		log.Printf("engine: failed to marshal %s message: %v", role, err)
		return
	}
	if err := e.store.AppendMessage(ctx, sessionID, &store.MessageRecord{
		Role:    string(role),
		Content: string(contentJSON),
	}); err != nil {
		log.Printf("engine: failed to persist %s message for session %s: %v", role, sessionID, err)
	}
}

// RunSubagent implements tool.EngineRunner for the subagent tool.
func (e *Engine) RunSubagent(ctx context.Context, systemPrompt, task string, onEvent func(provider.Event)) error {
	subCfg := *e.config
	subCfg.System = systemPrompt
	// Deep copy the Tools map to prevent mutation of parent config
	if e.config.Tools != nil {
		subCfg.Tools = make(map[string]config.ToolConfig, len(e.config.Tools))
		for k, v := range e.config.Tools {
			subCfg.Tools[k] = v
		}
	}

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.ContentBlock{
			{Type: "text", Text: task},
		}},
	}

	return e.loop(ctx, "", messages, &subCfg, onEvent)
}

// Run starts a conversation from the user's initial prompt.
func (e *Engine) Run(ctx context.Context, sessionID string, prompt string, onEvent func(provider.Event)) error {
	if e.snapMgr != nil {
		e.snapMgr.SetSession(sessionID)
	}
	userMsg := provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{
			{Type: "text", Text: prompt},
		},
	}

	e.persistMessage(ctx, sessionID, provider.RoleUser, userMsg.Content)

	messages := []provider.Message{userMsg}
	return e.loop(ctx, sessionID, messages, e.config, onEvent)
}

// Resume continues an existing session with a new user message.
func (e *Engine) Resume(ctx context.Context, sessionID string, prompt string, onEvent func(provider.Event)) error {
	if e.snapMgr != nil {
		e.snapMgr.SetSession(sessionID)
	}
	records, err := e.store.GetMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("loading messages: %w", err)
	}

	var messages []provider.Message
	for _, rec := range records {
		var content []provider.ContentBlock
		if err := json.Unmarshal([]byte(rec.Content), &content); err != nil {
			return fmt.Errorf("parsing stored message: %w", err)
		}
		messages = append(messages, provider.Message{
			Role:    provider.Role(rec.Role),
			Content: content,
		})
	}

	userMsg := provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.ContentBlock{{Type: "text", Text: prompt}},
	}
	e.persistMessage(ctx, sessionID, provider.RoleUser, userMsg.Content)

	messages = append(messages, userMsg)
	return e.loop(ctx, sessionID, messages, e.config, onEvent)
}

// loop is the core agentic loop.
func (e *Engine) loop(ctx context.Context, sessionID string, messages []provider.Message, cfg *config.Config, onEvent func(provider.Event)) error {
	system := cfg.System
	if system == "" {
		system = defaultSystem
	}

	// Auto-read project context file (nanocode.md) if it exists (bounded to 1MB).
	if cfg.ProjectDir != "" {
		if f, err := os.Open(filepath.Join(cfg.ProjectDir, "nanocode.md")); err == nil {
			const maxProjectCtx = 1 << 20 // 1MB
			data, readErr := io.ReadAll(io.LimitReader(f, maxProjectCtx+1))
			f.Close()
			if readErr == nil && len(data) > 0 {
				content := string(data)
				if len(data) > maxProjectCtx {
					content = content[:maxProjectCtx] + "\n... (truncated at 1MB)"
				}
				system += "\n\n# Project Context\n\n" + content
			}
		}
	}

	// Track per-file edits to detect doom loops (same file edited repeatedly).
	fileEditCounts := make(map[string]int)
	const maxFileEdits = 5

	for i := 0; i < maxIterations; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		windowed := windowMessages(messages, maxContextMessages)

		req := &provider.Request{
			Model:     cfg.Model,
			Messages:  windowed,
			Tools:     e.tools.Definitions(),
			MaxTokens: cfg.MaxTokens,
			System:    system,
		}

		events, err := e.provider.Stream(ctx, req)
		if err != nil {
			return fmt.Errorf("provider stream (iteration %d): %w", i+1, err)
		}

		assistantMsg, err := collectResponse(events, onEvent)
		if err != nil {
			return fmt.Errorf("collecting response (iteration %d): %w", i+1, err)
		}

		// Persist assistant message
		e.persistMessage(ctx, sessionID, provider.RoleAssistant, assistantMsg.Content)

		// Extract tool calls
		var toolCalls []*provider.ToolCall
		for _, cb := range assistantMsg.Content {
			if cb.Type == "tool_use" && cb.ToolCall != nil {
				toolCalls = append(toolCalls, cb.ToolCall)
			}
		}

		// If no tool calls, we are done
		if len(toolCalls) == 0 {
			return nil
		}

		// Execute tools with doom loop detection
		var resultBlocks []provider.ContentBlock
		for _, tc := range toolCalls {
			if tc.Name == "edit" || tc.Name == "write" {
				var inp struct {
					FilePath string `json:"file_path"`
				}
				if err := json.Unmarshal(tc.Input, &inp); err != nil {
					resultBlocks = append(resultBlocks, provider.ContentBlock{
						Type: "tool_result",
						ToolResult: &provider.ToolResult{
							ToolCallID: tc.ID,
							Content:    fmt.Sprintf("Failed to parse %s input: %v", tc.Name, err),
							IsError:    true,
						},
					})
					continue
				}
				if inp.FilePath != "" {
					key := filepath.Clean(inp.FilePath)
					if cfg.ProjectDir != "" && !filepath.IsAbs(key) {
						key = filepath.Clean(filepath.Join(cfg.ProjectDir, key))
					}
					fileEditCounts[key]++
					if fileEditCounts[key] > maxFileEdits {
						resultBlocks = append(resultBlocks, provider.ContentBlock{
							Type: "tool_result",
							ToolResult: &provider.ToolResult{
								ToolCallID: tc.ID,
								Content:    fmt.Sprintf("Doom loop detected: %s has been edited %d times. Stop and report to the user.", key, fileEditCounts[key]),
								IsError:    true,
							},
						})
						continue
					}
				}
			}

			result := e.tools.Execute(ctx, tc)
			resultBlocks = append(resultBlocks, provider.ContentBlock{
				Type:       "tool_result",
				ToolResult: result,
			})
		}

		resultMsg := provider.Message{Role: provider.RoleUser, Content: resultBlocks}

		// Persist tool results
		e.persistMessage(ctx, sessionID, provider.RoleUser, resultMsg.Content)

		messages = append(messages, *assistantMsg, resultMsg)
	}

	return fmt.Errorf("maximum iterations (%d) reached", maxIterations)
}

// windowMessages prevents context overflow by keeping the first message
// (original user prompt) and the last maxN messages. It adjusts the cut
// point to avoid splitting tool_use/tool_result pairs, which would cause
// API errors from both Anthropic and OpenAI.
func windowMessages(msgs []provider.Message, maxN int) []provider.Message {
	if len(msgs) <= maxN {
		return msgs
	}
	// Start index for the tail (after reserving slot 0 for the first message)
	startIdx := len(msgs) - (maxN - 1)

	// If we'd start on a tool_result message, back up to include
	// the preceding assistant message that contains the tool_use.
	if startIdx > 1 {
		msg := msgs[startIdx]
		hasToolResult := false
		for _, cb := range msg.Content {
			if cb.Type == "tool_result" {
				hasToolResult = true
				break
			}
		}
		if hasToolResult {
			startIdx--
		}
	}

	result := make([]provider.Message, 0, 1+len(msgs)-startIdx)
	result = append(result, msgs[0])
	result = append(result, msgs[startIdx:]...)
	return result
}

// collectResponse drains the event channel and builds the assistant message.
func collectResponse(events <-chan provider.Event, onEvent func(provider.Event)) (*provider.Message, error) {
	var textBuilder strings.Builder
	var toolCalls []*provider.ToolCall

	for ev := range events {
		if onEvent != nil {
			onEvent(ev)
		}

		switch ev.Type {
		case provider.EventTextDelta:
			textBuilder.WriteString(ev.Text)
		case provider.EventToolCallEnd:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, ev.ToolCall)
			}
		case provider.EventError:
			return nil, ev.Error
		}
	}

	msg := &provider.Message{Role: provider.RoleAssistant}
	if textBuilder.Len() > 0 {
		msg.Content = append(msg.Content, provider.ContentBlock{
			Type: "text",
			Text: textBuilder.String(),
		})
	}
	for _, tc := range toolCalls {
		msg.Content = append(msg.Content, provider.ContentBlock{
			Type:     "tool_use",
			ToolCall: tc,
		})
	}

	return msg, nil
}
