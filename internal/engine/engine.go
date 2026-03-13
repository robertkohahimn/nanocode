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
	"sync"
	"time"

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

// ToolRecord captures a single tool invocation during an engine run.
type ToolRecord struct {
	Name       string
	DurationMs int64
	IsError    bool
}

const errorReflectionPrompt = `<error-reflection>
The previous tool call failed. Before your next action:
1. What specific error occurred?
2. What assumption was incorrect?
3. What will you do differently this time?

Do NOT retry the exact same command. If you are stuck after 2 failed attempts at the same approach, ask the user for help.
</error-reflection>`

// Engine is the core conversation loop.
type Engine struct {
	provider         provider.Provider
	tools            *ToolRegistry
	store            store.Store
	config           *config.Config
	mcpClients       []io.Closer       // MCP subprocess handles
	snapMgr          *snapshot.Manager  // nil if no project dir
	mu               sync.Mutex        // protects lastRunRecords
	lastRunRecords   []ToolRecord
	currentSessionID string // set per Run/Resume for task tools
}

// New creates an Engine with the given dependencies.
// stdinReader is shared with the REPL loop to avoid conflicting buffered readers.
func New(p provider.Provider, s store.Store, cfg *config.Config, stdinReader *bufio.Reader, autoConfirm bool) *Engine {
	bashTool := tool.NewBashTool(stdinReader)
	bashTool.SetToolCallIDGetter(ToolCallIDFromContext)

	// Auto-confirm mode: skip interactive prompts
	if autoConfirm {
		bashTool.ConfirmFunc = func(command string) bool {
			return true
		}
	}

	// Permission system: wire allow/deny/autoApprove into bash confirm hook
	if bashCfg, ok := cfg.Tools["bash"]; ok {
		hasPermConfig := len(bashCfg.Allow) > 0 || len(bashCfg.Deny) > 0 || len(bashCfg.AutoApprove) > 0
		if hasPermConfig {
			checker := permission.NewChecker(bashCfg.Allow, bashCfg.Deny, bashCfg.AutoApprove)
			origConfirm := bashTool.ConfirmFunc
			bashTool.ConfirmFunc = func(cmd string) bool {
				result := checker.Check(cmd)
				if !result.Allowed {
					fmt.Fprintf(os.Stderr, "\033[31mBlocked:\033[0m %s\n", result.Reason)
					return false
				}
				if result.AutoApprove && !cfg.StrictMode {
					fmt.Fprintf(os.Stderr, "\033[32mAuto-approved:\033[0m %s\n", cmd)
					return true
				}
				return origConfirm(cmd)
			}
		}
	}

	// Snapshot tracking
	baseDir := cfg.ProjectDir
	var snapMgr *snapshot.Manager
	var onChange func(string)
	if baseDir != "" && !cfg.DisableSnapshot {
		snapMgr = snapshot.New(baseDir, s)
		onChange = snapMgr.Track
	}

	fileTracker := tool.NewFileTracker()
	writeTool := &tool.WriteTool{BaseDir: baseDir, OnChange: onChange, Tracker: fileTracker}
	editTool := &tool.EditTool{BaseDir: baseDir, OnChange: onChange, Tracker: fileTracker}

	// Collect built-in tools
	allTools := []tool.Tool{
		&tool.ReadTool{BaseDir: baseDir, Tracker: fileTracker},
		writeTool, editTool,
		&tool.GlobTool{},
		&tool.GrepTool{BaseDir: baseDir},
		bashTool,
	}

	// MCP tools
	const mcpStartupTimeout = 15 * time.Second
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
			initCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			err = client.Initialize(initCtx)
			cancel()
			if err != nil {
				client.Close()
				log.Printf("mcp: failed to initialize %s: %v", name, err)
				continue
			}
			listCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			tools, err := client.ListTools(listCtx)
			cancel()
			if err != nil {
				client.Close()
				log.Printf("mcp: failed to list tools from %s: %v", name, err)
				continue
			}
			mcpTools = client.Tools(name+"_", tools)
			mcpClients = append(mcpClients, client)
		case "http":
			client := mcp.NewHTTPClient(serverCfg.URL)
			initCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			err := client.Initialize(initCtx)
			cancel()
			if err != nil {
				log.Printf("mcp: failed to initialize %s: %v", name, err)
				continue
			}
			listCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			tools, err := client.ListTools(listCtx)
			cancel()
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

	getSessionID := func() string { return eng.currentSessionID }
	allTools = append(allTools,
		&tool.TaskCreateTool{Store: s, GetSessionID: getSessionID},
		&tool.TaskUpdateTool{Store: s, GetSessionID: getSessionID},
		&tool.TaskListTool{Store: s, GetSessionID: getSessionID},
		&tool.TaskGetTool{Store: s, GetSessionID: getSessionID},
	)

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

// ConsumeToolRecords returns and clears the tool records from the last run.
func (e *Engine) ConsumeToolRecords() []ToolRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	records := e.lastRunRecords
	e.lastRunRecords = nil
	return records
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
	// Deep copy the Tools map and slice fields to prevent mutation of parent config
	if e.config.Tools != nil {
		subCfg.Tools = make(map[string]config.ToolConfig, len(e.config.Tools))
		for k, v := range e.config.Tools {
			tc := config.ToolConfig{}
			if len(v.Allow) > 0 {
				tc.Allow = append([]string(nil), v.Allow...)
			}
			if len(v.Deny) > 0 {
				tc.Deny = append([]string(nil), v.Deny...)
			}
			if len(v.AutoApprove) > 0 {
				tc.AutoApprove = append([]string(nil), v.AutoApprove...)
			}
			subCfg.Tools[k] = tc
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
	e.currentSessionID = sessionID
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
	e.currentSessionID = sessionID
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
	e.mu.Lock()
	e.lastRunRecords = nil
	e.mu.Unlock()

	system := cfg.System
	if system == "" {
		system = DefaultSystemPrompt()
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

	// Structured logging for engine decisions
	logger := NewEngineLogger(sessionID, cfg.LogWriter)
	loopStart := time.Now()
	var iterations int
	defer func() {
		logger.LogSessionEnd(iterations+1, time.Since(loopStart))
	}()

	// Track per-file edits to detect doom loops (same file edited repeatedly).
	fileEditCounts := make(map[string]int)
	const maxFileEdits = 5

	for iterations = 0; iterations < maxIterations; iterations++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		windowed := windowMessages(messages, maxContextMessages)
		if len(windowed) < len(messages) {
			logger.LogContextWindow(len(messages), len(windowed))
		}

		req := &provider.Request{
			Model:     cfg.Model,
			Messages:  windowed,
			Tools:     e.tools.Definitions(),
			MaxTokens: cfg.MaxTokens,
			System:    system,
		}

		events, err := e.provider.Stream(ctx, req)
		if err != nil {
			return fmt.Errorf("provider stream (iteration %d): %w", iterations+1, err)
		}

		assistantMsg, err := collectResponse(events, onEvent)
		if err != nil {
			return fmt.Errorf("collecting response (iteration %d): %w", iterations+1, err)
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

		logger.LogIteration(iterations+1, len(toolCalls))

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
					logger.LogToolCall(tc.Name, 0, true)
					resultBlocks = append(resultBlocks, provider.ContentBlock{
						Type: "tool_result",
						ToolResult: &provider.ToolResult{
							ToolCallID: tc.ID,
							Content:    fmt.Sprintf("Failed to parse %s input: %v", tc.Name, err),
							IsError:    true,
						},
					})
					if !cfg.DisableReflection {
						resultBlocks = append(resultBlocks, provider.ContentBlock{
							Type: "text",
							Text: errorReflectionPrompt,
						})
					}
					continue
				}
				if inp.FilePath != "" {
					key := filepath.Clean(inp.FilePath)
					if cfg.ProjectDir != "" && !filepath.IsAbs(key) {
						key = filepath.Clean(filepath.Join(cfg.ProjectDir, key))
					}
					fileEditCounts[key]++
					if fileEditCounts[key] > maxFileEdits {
						logger.LogToolCall(tc.Name, 0, true)
						logger.LogDoomLoop(key, fileEditCounts[key])
						resultBlocks = append(resultBlocks, provider.ContentBlock{
							Type: "tool_result",
							ToolResult: &provider.ToolResult{
								ToolCallID: tc.ID,
								Content:    fmt.Sprintf("Doom loop detected: %s has been edited %d times. Stop and report to the user.", key, fileEditCounts[key]),
								IsError:    true,
							},
						})
						if !cfg.DisableReflection {
							resultBlocks = append(resultBlocks, provider.ContentBlock{
								Type: "text",
								Text: errorReflectionPrompt,
							})
						}
						continue
					}
				}
			}

			toolStart := time.Now()
			result := e.tools.Execute(ctx, tc)
			elapsed := time.Since(toolStart)
			logger.LogToolCall(tc.Name, elapsed, result.IsError)
			e.mu.Lock()
			e.lastRunRecords = append(e.lastRunRecords, ToolRecord{
				Name:       tc.Name,
				DurationMs: elapsed.Milliseconds(),
				IsError:    result.IsError,
			})
			e.mu.Unlock()
			resultBlocks = append(resultBlocks, provider.ContentBlock{
				Type:       "tool_result",
				ToolResult: result,
			})
			if result.IsError && !cfg.DisableReflection {
				resultBlocks = append(resultBlocks, provider.ContentBlock{
					Type: "text",
					Text: errorReflectionPrompt,
				})
			}
		}

		resultMsg := provider.Message{Role: provider.RoleUser, Content: resultBlocks}

		// Persist tool results
		e.persistMessage(ctx, sessionID, provider.RoleUser, resultMsg.Content)

		messages = append(messages, *assistantMsg, resultMsg)
	}

	return fmt.Errorf("maximum iterations (%d) reached", maxIterations)
}
