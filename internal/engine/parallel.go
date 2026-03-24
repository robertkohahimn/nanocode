package engine

import (
	"context"
	"sync"
	"time"

	"github.com/robertkohahimn/nanocode/internal/config"
	"github.com/robertkohahimn/nanocode/internal/provider"
)

var readOnlyTools = map[string]bool{
	"read": true,
	"glob": true,
	"grep": true,
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
