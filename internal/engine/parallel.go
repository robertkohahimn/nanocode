package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/robertkohahimn/nanocode/internal/config"
	"github.com/robertkohahimn/nanocode/internal/provider"
)

var readOnlyTools = map[string]bool{
	"read":        true,
	"glob":        true,
	"grep":        true,
	"task_output": true,
}

type toolCallGroup struct {
	parallel bool
	calls    []*provider.ToolCall
}

func partitionToolCalls(calls []*provider.ToolCall) []toolCallGroup {
	if len(calls) == 0 {
		return nil
	}
	var groups []toolCallGroup
	var batch []*provider.ToolCall

	for _, tc := range calls {
		if readOnlyTools[tc.Name] {
			batch = append(batch, tc)
		} else {
			if len(batch) > 0 {
				groups = append(groups, toolCallGroup{parallel: true, calls: batch})
				batch = nil
			}
			groups = append(groups, toolCallGroup{parallel: false, calls: []*provider.ToolCall{tc}})
		}
	}
	if len(batch) > 0 {
		groups = append(groups, toolCallGroup{parallel: true, calls: batch})
	}
	return groups
}

const maxParallelTools = 10

type parallelResult struct {
	provider.ContentBlock
	elapsed time.Duration
}

func executeParallelBatch(ctx context.Context, reg *ToolRegistry, calls []*provider.ToolCall) []parallelResult {
	results := make([]parallelResult, len(calls))
	sem := make(chan struct{}, maxParallelTools)
	var wg sync.WaitGroup

	for i, tc := range calls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, call *provider.ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[idx] = parallelResult{
						ContentBlock: provider.ContentBlock{
							Type: "tool_result",
							ToolResult: &provider.ToolResult{
								ToolCallID: call.ID,
								Content:    fmt.Sprintf("tool panic: %v", r),
								IsError:    true,
							},
						},
					}
				}
			}()
			start := time.Now()
			result := reg.Execute(ctx, call)
			results[idx] = parallelResult{
				ContentBlock: provider.ContentBlock{
					Type:       "tool_result",
					ToolResult: result,
				},
				elapsed: time.Since(start),
			}
		}(i, tc)
	}

	wg.Wait()
	return results
}

type toolExecContext struct {
	engine       *Engine
	cfg          *config.Config
	loopDetector *LoopDetector
	verifyState  *VerifyState
	fc           *FailureCollector
	logger       *EngineLogger
	iteration    int
}

func executeToolCalls(ctx context.Context, tec *toolExecContext, toolCalls []*provider.ToolCall) []provider.ContentBlock {
	groups := partitionToolCalls(toolCalls)
	var resultBlocks []provider.ContentBlock

	for _, group := range groups {
		if group.parallel {
			results := executeParallelBatch(ctx, tec.engine.tools, group.calls)
			for i, cb := range results {
				tc := group.calls[i]
				isError := cb.ToolResult != nil && cb.ToolResult.IsError
				elapsed := cb.elapsed
				tec.logger.LogToolCall(tc.Name, elapsed, isError)
				tec.fc.TrackTool(tc.Name)
				tec.engine.mu.Lock()
				tec.engine.lastRunRecords = append(tec.engine.lastRunRecords, ToolRecord{
					Name: tc.Name, DurationMs: elapsed.Milliseconds(), IsError: isError,
				})
				tec.engine.mu.Unlock()
				resultBlocks = append(resultBlocks, cb.ContentBlock)
				if isError && !tec.cfg.DisableReflection {
					resultBlocks = append(resultBlocks, provider.ContentBlock{
						Type: "text", Text: errorReflectionPrompt,
					})
				}
			}
			continue
		}

		for _, tc := range group.calls {
			blocks := executeSequentialTool(ctx, tec, tc)
			resultBlocks = append(resultBlocks, blocks...)
		}
	}
	return resultBlocks
}

func executeSequentialTool(ctx context.Context, tec *toolExecContext, tc *provider.ToolCall) []provider.ContentBlock {
	var resultBlocks []provider.ContentBlock
	injectWarning := func(w *LoopWarning) {
		if !tec.cfg.DisableReflection {
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: FormatWarning(w)})
		}
	}

	if tc.Name == "edit" || tc.Name == "write" {
		var inp struct {
			FilePath  string `json:"file_path"`
			Content   string `json:"content"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(tc.Input, &inp); err != nil {
			tec.logger.LogToolCall(tc.Name, 0, true)
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: &provider.ToolResult{
				ToolCallID: tc.ID, Content: fmt.Sprintf("Failed to parse %s input: %v", tc.Name, err), IsError: true,
			}})
			if !tec.cfg.DisableReflection {
				resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: errorReflectionPrompt})
			}
			return resultBlocks
		}
		if inp.FilePath != "" {
			key := filepath.Clean(inp.FilePath)
			if tec.cfg.ProjectDir != "" && !filepath.IsAbs(key) {
				key = filepath.Clean(filepath.Join(tec.cfg.ProjectDir, key))
			}
			editContent := inp.Content
			if tc.Name == "edit" {
				editContent = inp.NewString
			}
			if w := tec.loopDetector.CheckEdit(key, editContent); w != nil {
				if w.Type == "edit_count" {
					tec.logger.LogToolCall(tc.Name, 0, true)
					tec.logger.LogDoomLoop(key, tec.loopDetector.editCounts[key])
					tec.fc.TrackFile(key)
					tec.fc.Record(ctx, FailureDoomLoop, fmt.Sprintf("file %s edited too many times", key), tec.iteration+1)
					resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: &provider.ToolResult{
						ToolCallID: tc.ID, Content: w.Detail, IsError: true,
					}})
					injectWarning(w)
					return resultBlocks
				}
				injectWarning(w)
			}
		}
	}
	if tc.Name == "bash" {
		var inp struct{ Command string `json:"command"` }
		if err := json.Unmarshal(tc.Input, &inp); err == nil && inp.Command != "" {
			if w := tec.loopDetector.CheckCommand(inp.Command); w != nil {
				injectWarning(w)
			}
		}
	}
	toolStart := time.Now()
	result := tec.engine.tools.Execute(ctx, tc)
	elapsed := time.Since(toolStart)
	tec.logger.LogToolCall(tc.Name, elapsed, result.IsError)
	tec.fc.TrackTool(tc.Name)
	tec.engine.mu.Lock()
	tec.engine.lastRunRecords = append(tec.engine.lastRunRecords, ToolRecord{
		Name: tc.Name, DurationMs: elapsed.Milliseconds(), IsError: result.IsError,
	})
	tec.engine.mu.Unlock()
	resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "tool_result", ToolResult: result})
	if !result.IsError {
		if tc.Name == "edit" || tc.Name == "write" {
			var inp struct{ FilePath string `json:"file_path"` }
			if json.Unmarshal(tc.Input, &inp) == nil && inp.FilePath != "" {
				tec.verifyState.MarkEdit(inp.FilePath)
			}
		}
		if tc.Name == "bash" {
			var inp struct {
				Command         string `json:"command"`
				RunInBackground bool   `json:"run_in_background"`
			}
			if json.Unmarshal(tc.Input, &inp) == nil && !inp.RunInBackground && IsVerifyCommand(inp.Command) {
				tec.verifyState.MarkVerified()
			}
		}
	}
	if result.IsError && !tec.cfg.DisableReflection {
		if w := tec.loopDetector.CheckError(result.Content); w != nil {
			injectWarning(w)
		} else {
			resultBlocks = append(resultBlocks, provider.ContentBlock{Type: "text", Text: errorReflectionPrompt})
		}
	}
	return resultBlocks
}
