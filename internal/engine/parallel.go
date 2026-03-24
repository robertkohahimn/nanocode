package engine

import (
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

type toolExecContext struct {
	engine       *Engine
	cfg          *config.Config
	loopDetector *LoopDetector
	verifyState  *VerifyState
	fc           *FailureCollector
	logger       *EngineLogger
	iteration    int
}
