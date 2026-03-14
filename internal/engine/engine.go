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
		&tool.AskUserQuestionTool{StdinReader: stdinReader},
	}

	// MCP tools
	mcpTools, mcpClients := loadMCPTools(cfg)
	allTools = append(allTools, mcpTools...)

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

	if projectCtx := BuildProjectContext(cfg.ProjectDir); projectCtx != "" {
		system += "\n\n# Project Context\n\n" + projectCtx
	}

	// Structured logging for engine decisions
	logger := NewEngineLogger(sessionID, cfg.LogWriter)
	loopStart := time.Now()
	var iterations int
	defer func() {
		logger.LogSessionEnd(iterations+1, time.Since(loopStart))
	}()

	// Failure collection: track tool usage and record failures.
	fc := NewFailureCollector(e.store, sessionID)

	// Semantic doom loop detector replaces the old per-file edit counter.
	loopDetector := NewLoopDetector()

	// Track verification state for edit-then-verify enforcement.
	verifyState := &VerifyState{}

	checkpoint := NewCheckpointInjector(cfg.CheckpointInterval)
	summarizer := NewSummarizer(e.provider, cfg.SummarizeThreshold, cfg.SummarizeKeepRecent)

	for iterations = 0; iterations < maxIterations; iterations++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if cb, level := checkpoint.MaybeInject(iterations); cb != nil {
			logger.LogCheckpoint(iterations, level)
			messages = append(messages, provider.Message{
				Role:    provider.RoleUser,
				Content: []provider.ContentBlock{*cb},
			})
		}

		// Summarize if enabled, then apply windowing as safety net.
		summarized, sumErr := summarizer.MaybeSummarize(ctx, messages)
		if sumErr != nil {
			log.Printf("engine: summarization error: %v", sumErr)
			summarized = messages
		}
		if len(summarized) < len(messages) {
			logger.LogSummarization(len(messages), len(summarized))
		}
		windowed := windowMessages(summarized, maxContextMessages)
		if len(windowed) < len(summarized) {
			logger.LogContextWindow(len(summarized), len(windowed))
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

		// If no tool calls, check verification before finishing
		if len(toolCalls) == 0 {
			if verifyState.IsPending() && !cfg.DisableVerification {
				reminderMsg := provider.Message{
					Role:    provider.RoleUser,
					Content: []provider.ContentBlock{{Type: "text", Text: verifyState.ReminderText()}},
				}
				messages = append(messages, *assistantMsg, reminderMsg)
				verifyState.MarkVerified() // Only remind once
				continue
			}
			return nil
		}

		// Execute tools with semantic doom loop detection
		var resultBlocks []provider.ContentBlock
		injectWarning := func(w *LoopWarning) {
			if !cfg.DisableReflection {
				resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: FormatWarning(w)})
			}
		}
		for _, tc := range toolCalls {
			if tc.Name == "edit" || tc.Name == "write" {
				var inp struct {
					FilePath  string `json:"file_path"`
					Content   string `json:"content"`
					NewString string `json:"new_string"`
				}
				if err := json.Unmarshal(tc.Input, &inp); err != nil {
					logger.LogToolCall(tc.Name, 0, true)
					resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: &provider.ToolResult{
						ToolCallID: tc.ID, Content: fmt.Sprintf("Failed to parse %s input: %v", tc.Name, err), IsError: true,
					}})
					if !cfg.DisableReflection {
						resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: errorReflectionPrompt})
					}
					continue
				}
				if inp.FilePath != "" {
					key := filepath.Clean(inp.FilePath)
					if cfg.ProjectDir != "" && !filepath.IsAbs(key) {
						key = filepath.Clean(filepath.Join(cfg.ProjectDir, key))
					}
					editContent := inp.Content
					if tc.Name == "edit" {
						editContent = inp.NewString
					}
					if w := loopDetector.CheckEdit(key, editContent); w != nil {
						if w.Type == "edit_count" {
							logger.LogToolCall(tc.Name, 0, true)
							logger.LogDoomLoop(key, loopDetector.editCounts[key])
							fc.TrackFile(key)
							fc.Record(ctx, FailureDoomLoop, fmt.Sprintf("file %s edited too many times", key), iterations+1)
							resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: &provider.ToolResult{
								ToolCallID: tc.ID, Content: w.Detail, IsError: true,
							}})
							injectWarning(w)
							continue
						}
						injectWarning(w)
					}
				}
			}
			if tc.Name == "bash" {
				var inp struct{ Command string `json:"command"` }
				if err := json.Unmarshal(tc.Input, &inp); err == nil && inp.Command != "" {
					if w := loopDetector.CheckCommand(inp.Command); w != nil {
						injectWarning(w)
					}
				}
			}
			toolStart := time.Now()
			result := e.tools.Execute(ctx, tc)
			elapsed := time.Since(toolStart)
			logger.LogToolCall(tc.Name, elapsed, result.IsError)
			fc.TrackTool(tc.Name)
			e.mu.Lock()
			e.lastRunRecords = append(e.lastRunRecords, ToolRecord{
				Name: tc.Name, DurationMs: elapsed.Milliseconds(), IsError: result.IsError,
			})
			e.mu.Unlock()
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: result})
			// Track verification state
			if !result.IsError {
				if tc.Name == "edit" || tc.Name == "write" {
					var inp struct{ FilePath string `json:"file_path"` }
					if json.Unmarshal(tc.Input, &inp) == nil && inp.FilePath != "" {
						verifyState.MarkEdit(inp.FilePath)
					}
				}
				if tc.Name == "bash" {
					var inp struct{ Command string `json:"command"` }
					if json.Unmarshal(tc.Input, &inp) == nil && IsVerifyCommand(inp.Command) {
						verifyState.MarkVerified()
					}
				}
			}
			if result.IsError && !cfg.DisableReflection {
				if w := loopDetector.CheckError(result.Content); w != nil {
					injectWarning(w)
				} else {
					resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: errorReflectionPrompt})
				}
			}
		}

		resultMsg := provider.Message{Role: provider.RoleUser, Content: resultBlocks}

		// Persist tool results
		e.persistMessage(ctx, sessionID, provider.RoleUser, resultMsg.Content)

		messages = append(messages, *assistantMsg, resultMsg)
	}

	fc.Record(ctx, FailureMaxIter, fmt.Sprintf("reached %d iterations", maxIterations), maxIterations)
	return fmt.Errorf("maximum iterations (%d) reached", maxIterations)
}
