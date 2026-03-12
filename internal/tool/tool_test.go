package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
)

func TestParseInput(t *testing.T) {
	type testInput struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	v, err := ParseInput[testInput](json.RawMessage(`{"name":"Alice","age":30}`))
	if err != nil {
		t.Fatalf("ParseInput: %v", err)
	}
	if v.Name != "Alice" || v.Age != 30 {
		t.Errorf("got %+v", v)
	}

	_, err = ParseInput[testInput](json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTruncateOutput(t *testing.T) {
	short := "hello"
	if TruncateOutput(short, 100) != short {
		t.Error("should not truncate short string")
	}

	long := strings.Repeat("x", 200)
	result := TruncateOutput(long, 100)
	if len(result) > 120 {
		t.Errorf("truncated result too long: %d", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Error("should contain truncation marker")
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("hello world")) {
		t.Error("text should not be binary")
	}
	if !IsBinary([]byte{0x00, 0x01, 0x02}) {
		t.Error("null bytes should be binary")
	}
}

func TestSkipDir(t *testing.T) {
	for _, name := range []string{".git", "node_modules", "vendor", "__pycache__"} {
		if !SkipDir(name) {
			t.Errorf("%q should be skipped", name)
		}
	}
	if SkipDir("src") {
		t.Error("src should not be skipped")
	}
}

// --- ReadTool tests ---

func TestReadToolBasic(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.txt")
	os.WriteFile(fp, []byte("line1\nline2\nline3\n"), 0644)

	rt := &ReadTool{}
	result, err := rt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "line1") || !strings.Contains(result, "line3") {
		t.Errorf("expected all lines, got %q", result)
	}
}

func TestReadToolOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.txt")
	os.WriteFile(fp, []byte("a\nb\nc\nd\ne\n"), 0644)

	rt := &ReadTool{}
	result, err := rt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","offset":2,"limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "b") || !strings.Contains(result, "c") {
		t.Errorf("expected lines 2-3, got %q", result)
	}
	if strings.Contains(result, "\t"+"a\n") {
		t.Error("should not contain line 1")
	}
}

func TestReadToolMissingFile(t *testing.T) {
	rt := &ReadTool{}
	_, err := rt.Execute(context.Background(), json.RawMessage(`{"file_path":"/nonexistent/file.txt"}`))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadToolBinary(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "binary.dat")
	os.WriteFile(fp, []byte{0x00, 0x01, 0x02, 0x03}, 0644)

	rt := &ReadTool{}
	result, err := rt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Binary file") {
		t.Errorf("expected binary message, got %q", result)
	}
}

// --- WriteTool tests ---

func TestWriteToolBasic(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "out.txt")

	wt := &WriteTool{}
	result, err := wt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","content":"hello world"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "11 bytes") {
		t.Errorf("unexpected result: %q", result)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "hello world" {
		t.Errorf("file content: %q", string(data))
	}
}

func TestWriteToolCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "deep", "nested", "file.txt")

	wt := &WriteTool{}
	_, err := wt.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","content":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "ok" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

// --- EditTool tests ---

func TestEditToolSingleMatch(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")
	os.WriteFile(fp, []byte("hello world"), 0644)

	et := &EditTool{}
	result, err := et.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","old_string":"hello","new_string":"goodbye"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "1 replacement") {
		t.Errorf("unexpected result: %q", result)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "goodbye world" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEditToolMultipleMatchError(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")
	os.WriteFile(fp, []byte("aaa bbb aaa"), 0644)

	et := &EditTool{}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","old_string":"aaa","new_string":"ccc"}`))
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
}

func TestEditToolReplaceAll(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")
	os.WriteFile(fp, []byte("aaa bbb aaa"), 0644)

	et := &EditTool{}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","old_string":"aaa","new_string":"ccc","replace_all":true}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(fp)
	if string(data) != "ccc bbb ccc" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEditToolNoMatch(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")
	os.WriteFile(fp, []byte("hello"), 0644)

	et := &EditTool{}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"file_path":"`+fp+`","old_string":"xyz","new_string":"abc"}`))
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

// --- GlobTool tests ---

func TestGlobToolBasic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "c.go"), nil, 0644)

	gt := &GlobTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"**/*.go","path":"`+dir+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "a.go") || !strings.Contains(result, "c.go") {
		t.Errorf("expected .go files, got %q", result)
	}
	if strings.Contains(result, "b.txt") {
		t.Error("should not match .txt")
	}
}

func TestGlobToolNoMatches(t *testing.T) {
	dir := t.TempDir()
	gt := &GlobTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"*.xyz","path":"`+dir+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "No files matched" {
		t.Errorf("expected no matches, got %q", result)
	}
}

// --- GrepTool tests ---

func TestGrepToolBasic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("func main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)

	gt := &GrepTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"func","path":"`+dir+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "test.go:1:func main()") {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestGrepToolCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("Hello World\n"), 0644)

	gt := &GrepTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"hello","path":"`+dir+`","case_insensitive":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Hello World") {
		t.Errorf("expected match, got %q", result)
	}
}

func TestGrepToolNoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("nothing here\n"), 0644)

	gt := &GrepTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"xyz123","path":"`+dir+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "No matches found" {
		t.Errorf("expected no matches, got %q", result)
	}
}

// --- BashTool tests ---

func TestBashToolBasic(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected hello, got %q", result)
	}
}

func TestBashToolNonZeroExit(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"exit 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Exit code 1") {
		t.Errorf("expected exit code 1, got %q", result)
	}
}

func TestBashToolRejected(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return false }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "rejected") {
		t.Errorf("expected rejected, got %q", result)
	}
}

func TestBashToolTimeout(t *testing.T) {
	bt := &BashTool{ConfirmFunc: func(string) bool { return true }}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"sleep 60","timeout":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "timed out") {
		t.Errorf("expected timeout, got %q", result)
	}
}

// --- SubagentTool tests ---

type mockRunner struct {
	systemPrompt string
	task         string
	output       string
	err          error
}

func (m *mockRunner) RunSubagent(ctx context.Context, systemPrompt, task string, onEvent func(provider.Event)) error {
	m.systemPrompt = systemPrompt
	m.task = task
	if m.err != nil {
		return m.err
	}
	onEvent(provider.Event{Type: provider.EventTextDelta, Text: m.output})
	return nil
}

func TestSubagentToolBasic(t *testing.T) {
	runner := &mockRunner{output: "done"}
	st := &SubagentTool{Runner: runner}
	result, err := st.Execute(context.Background(), json.RawMessage(`{"task":"do something"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Errorf("expected 'done', got %q", result)
	}
	if runner.task != "do something" {
		t.Errorf("expected task 'do something', got %q", runner.task)
	}
}

func TestSubagentToolDepthLimit(t *testing.T) {
	runner := &mockRunner{output: "ok"}
	st := &SubagentTool{Runner: runner}
	ctx := context.WithValue(context.Background(), depthKey, 3)
	_, err := st.Execute(ctx, json.RawMessage(`{"task":"deep"}`))
	if err == nil {
		t.Fatal("expected depth limit error")
	}
}

func TestSubagentToolWithContext(t *testing.T) {
	runner := &mockRunner{output: "ok"}
	st := &SubagentTool{Runner: runner}
	result, err := st.Execute(context.Background(), json.RawMessage(`{"task":"do it","context":"extra info"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
	if !strings.Contains(runner.systemPrompt, "extra info") {
		t.Errorf("context not in system prompt: %q", runner.systemPrompt)
	}
}

// --- matchGlob tests ---

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.txt", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "sub/main.go", true},
		{"**/*.go", "sub/deep/main.go", true},
		{"src/**/*.go", "src/main.go", true},
		{"src/**/*.go", "src/pkg/main.go", true},
		{"src/**/*.go", "other/main.go", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.path)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

// --- BashTool confirmation override tests ---

// mockToolCallIDContext creates a context with a mock tool call ID getter
func mockToolCallIDContext(id string) context.Context {
	return context.WithValue(context.Background(), "tool_call_id", id)
}

func TestBashTool_ConfirmOverride_Approved(t *testing.T) {
	bt := NewBashTool(nil)
	// Mock the tool call ID getter to return our test ID
	bt.SetToolCallIDGetter(func(ctx context.Context) string {
		if id, ok := ctx.Value("tool_call_id").(string); ok {
			return id
		}
		return ""
	})
	// Set an override that approves without prompting
	bt.SetConfirmOverride("test-id", true, false)
	defer bt.ClearConfirmOverrides()

	// Execute should succeed without calling ConfirmFunc
	confirmCalled := false
	bt.ConfirmFunc = func(string) bool {
		confirmCalled = true
		return false // would reject if called
	}

	ctx := mockToolCallIDContext("test-id")
	result, err := bt.Execute(ctx, json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalled {
		t.Error("ConfirmFunc should not have been called")
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected hello, got %q", result)
	}
}

func TestBashTool_ConfirmOverride_Skipped(t *testing.T) {
	bt := NewBashTool(nil)
	bt.SetToolCallIDGetter(func(ctx context.Context) string {
		if id, ok := ctx.Value("tool_call_id").(string); ok {
			return id
		}
		return ""
	})
	bt.SetConfirmOverride("test-id", false, true) // skipped
	defer bt.ClearConfirmOverrides()

	ctx := mockToolCallIDContext("test-id")
	result, err := bt.Execute(ctx, json.RawMessage(`{"command":"echo nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "skipped") {
		t.Errorf("expected skipped message, got %q", result)
	}
}

func TestBashTool_NoOverride_FallsBack(t *testing.T) {
	bt := NewBashTool(nil)
	bt.SetToolCallIDGetter(func(ctx context.Context) string {
		if id, ok := ctx.Value("tool_call_id").(string); ok {
			return id
		}
		return ""
	})
	// Set override for different ID
	bt.SetConfirmOverride("other-id", true, false)
	defer bt.ClearConfirmOverrides()

	confirmCalled := false
	bt.ConfirmFunc = func(string) bool {
		confirmCalled = true
		return true
	}

	// Execute with ID that has no override
	ctx := mockToolCallIDContext("test-id")
	result, err := bt.Execute(ctx, json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !confirmCalled {
		t.Error("ConfirmFunc should have been called for non-overridden ID")
	}
	if !strings.Contains(result, "hi") {
		t.Errorf("expected hi, got %q", result)
	}
}
