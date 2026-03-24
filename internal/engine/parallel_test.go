package engine

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

func tc(name string) *provider.ToolCall {
	return &provider.ToolCall{ID: name + "-id", Name: name}
}

func TestPartitionToolCalls_AllReadOnly(t *testing.T) {
	calls := []*provider.ToolCall{tc("read"), tc("glob"), tc("grep")}
	groups := partitionToolCalls(calls)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !groups[0].parallel {
		t.Fatal("expected parallel group")
	}
	if len(groups[0].calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(groups[0].calls))
	}
}

func TestPartitionToolCalls_AllSequential(t *testing.T) {
	calls := []*provider.ToolCall{tc("edit"), tc("bash")}
	groups := partitionToolCalls(calls)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	for i, g := range groups {
		if g.parallel {
			t.Fatalf("group %d should not be parallel", i)
		}
		if len(g.calls) != 1 {
			t.Fatalf("group %d: expected 1 call, got %d", i, len(g.calls))
		}
	}
}

func TestPartitionToolCalls_Mixed(t *testing.T) {
	calls := []*provider.ToolCall{
		tc("read"), tc("glob"), tc("edit"), tc("read"), tc("read"), tc("bash"),
	}
	groups := partitionToolCalls(calls)
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}

	// Group 0: parallel(read, glob)
	if !groups[0].parallel || len(groups[0].calls) != 2 {
		t.Fatalf("group 0: expected parallel with 2 calls, got parallel=%v len=%d",
			groups[0].parallel, len(groups[0].calls))
	}
	// Group 1: sequential(edit)
	if groups[1].parallel || len(groups[1].calls) != 1 {
		t.Fatalf("group 1: expected sequential with 1 call, got parallel=%v len=%d",
			groups[1].parallel, len(groups[1].calls))
	}
	// Group 2: parallel(read, read)
	if !groups[2].parallel || len(groups[2].calls) != 2 {
		t.Fatalf("group 2: expected parallel with 2 calls, got parallel=%v len=%d",
			groups[2].parallel, len(groups[2].calls))
	}
	// Group 3: sequential(bash)
	if groups[3].parallel || len(groups[3].calls) != 1 {
		t.Fatalf("group 3: expected sequential with 1 call, got parallel=%v len=%d",
			groups[3].parallel, len(groups[3].calls))
	}
}

func TestPartitionToolCalls_Empty(t *testing.T) {
	groups := partitionToolCalls(nil)
	if groups != nil {
		t.Fatalf("expected nil, got %v", groups)
	}
}

type sleepTool struct {
	name     string
	duration time.Duration
	calls    atomic.Int32
}

func (s *sleepTool) Name() string { return s.name }
func (s *sleepTool) Definition() provider.ToolDef {
	return provider.ToolDef{Name: s.name, InputSchema: json.RawMessage(`{}`)}
}
func (s *sleepTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	s.calls.Add(1)
	time.Sleep(s.duration)
	return "ok", nil
}

func TestExecuteParallelBatch(t *testing.T) {
	st := &sleepTool{name: "read", duration: 50 * time.Millisecond}
	reg := NewToolRegistry(st)

	calls := []*provider.ToolCall{
		{ID: "1", Name: "read", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "read", Input: json.RawMessage(`{}`)},
		{ID: "3", Name: "read", Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := executeParallelBatch(context.Background(), reg, calls)
	elapsed := time.Since(start)

	if elapsed > 120*time.Millisecond {
		t.Errorf("expected parallel execution, took %v", elapsed)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, r := range results {
		if r.ToolResult == nil || r.ToolResult.ToolCallID != calls[i].ID {
			t.Errorf("result %d: expected tool call ID %s", i, calls[i].ID)
		}
		if r.elapsed == 0 {
			t.Errorf("result %d: expected non-zero elapsed time", i)
		}
	}
	if st.calls.Load() != 3 {
		t.Errorf("expected 3 tool calls, got %d", st.calls.Load())
	}
}

func TestExecuteParallelBatch_OrderPreserved(t *testing.T) {
	fast := &sleepTool{name: "read", duration: 10 * time.Millisecond}
	slow := &sleepTool{name: "glob", duration: 50 * time.Millisecond}
	reg := NewToolRegistry(fast, slow)

	calls := []*provider.ToolCall{
		{ID: "slow", Name: "glob", Input: json.RawMessage(`{}`)},
		{ID: "fast", Name: "read", Input: json.RawMessage(`{}`)},
	}

	results := executeParallelBatch(context.Background(), reg, calls)
	if results[0].ToolResult.ToolCallID != "slow" {
		t.Error("expected first result to be 'slow' (order preserved)")
	}
	if results[1].ToolResult.ToolCallID != "fast" {
		t.Error("expected second result to be 'fast' (order preserved)")
	}
}

func TestPartitionToolCalls_SingleReadOnly(t *testing.T) {
	calls := []*provider.ToolCall{tc("read")}
	groups := partitionToolCalls(calls)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !groups[0].parallel {
		t.Fatal("expected parallel group")
	}
	if len(groups[0].calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(groups[0].calls))
	}
}
