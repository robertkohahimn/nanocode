package engine

import (
	"testing"

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
