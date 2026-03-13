package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockEngine implements EngineRunner for testing.
type mockEngine struct {
	records []ToolCallRecord
	err     error
}

func (m *mockEngine) Run(_ context.Context, _ string) ([]ToolCallRecord, error) {
	return m.records, m.err
}

func TestLoadTask(t *testing.T) {
	dir := t.TempDir()

	// Write required files
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("Do something\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("exit 0\n"), 0o644)

	task, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}

	if task.Prompt != "Do something" {
		t.Errorf("prompt = %q, want %q", task.Prompt, "Do something")
	}
	if task.VerifyScript != "exit 0\n" {
		t.Errorf("verify script = %q, want %q", task.VerifyScript, "exit 0\n")
	}
	if task.SetupScript != "" {
		t.Errorf("setup script should be empty, got %q", task.SetupScript)
	}
	if task.ID != filepath.Base(dir) {
		t.Errorf("id = %q, want %q", task.ID, filepath.Base(dir))
	}
}

func TestLoadTaskWithConfig(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("Fix it"), 0o644)
	os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("exit 0"), 0o644)
	os.WriteFile(filepath.Join(dir, "setup.sh"), []byte("echo setup"), 0o644)

	cfg := taskConfig{Category: "debugging", ExpectedTools: []string{"bash", "write"}}
	cfgBytes, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(dir, "config.json"), cfgBytes, 0o644)

	task, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if task.Category != "debugging" {
		t.Errorf("category = %q, want %q", task.Category, "debugging")
	}
	if task.SetupScript != "echo setup" {
		t.Errorf("setup = %q, want %q", task.SetupScript, "echo setup")
	}
	if len(task.ExpectedTools) != 2 {
		t.Errorf("expected_tools len = %d, want 2", len(task.ExpectedTools))
	}
}

func TestLoadTaskMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("exit 0"), 0o644)

	_, err := LoadTask(dir)
	if err == nil {
		t.Fatal("expected error for missing prompt.txt")
	}
}

func TestLoadTaskMissingVerify(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("hello"), 0o644)

	_, err := LoadTask(dir)
	if err == nil {
		t.Fatal("expected error for missing verify.sh")
	}
}

func TestLoadSuite(t *testing.T) {
	dir := t.TempDir()

	// Create two task subdirectories
	for _, name := range []string{"001-first", "002-second"} {
		taskDir := filepath.Join(dir, name)
		os.Mkdir(taskDir, 0o755)
		os.WriteFile(filepath.Join(taskDir, "prompt.txt"), []byte("Task "+name), 0o644)
		os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("exit 0"), 0o644)
	}

	suite, err := LoadSuite(dir)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if len(suite.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(suite.Tasks))
	}
	if suite.Tasks[0].ID != "001-first" {
		t.Errorf("first task id = %q, want %q", suite.Tasks[0].ID, "001-first")
	}
	if suite.Tasks[1].ID != "002-second" {
		t.Errorf("second task id = %q, want %q", suite.Tasks[1].ID, "002-second")
	}
	// Category defaults to suite name
	if suite.Tasks[0].Category != suite.Name {
		t.Errorf("category = %q, want %q", suite.Tasks[0].Category, suite.Name)
	}
}

type mockRetryEngine struct {
	attempts  *int
	failUntil int
}

func (m *mockRetryEngine) Run(_ context.Context, _ string) ([]ToolCallRecord, error) {
	*m.attempts++
	if *m.attempts <= m.failUntil {
		return nil, fmt.Errorf("anthropic API error 429: rate limit exceeded")
	}
	return []ToolCallRecord{{Name: "read", DurationMs: 10}}, nil
}

func TestRunTaskRetries429(t *testing.T) {
	attempts := 0
	runner := &Runner{
		EngineFactory: func(_ string) (EngineRunner, error) {
			return &mockRetryEngine{attempts: &attempts, failUntil: 2}, nil
		},
	}
	task := Task{ID: "retry-test", Prompt: "test", VerifyScript: "exit 0"}
	result := runner.RunTask(context.Background(), task)
	if !result.Passed {
		t.Errorf("expected passed after retries, got error: %s", result.Error)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRunTaskNoRetryOnOtherErrors(t *testing.T) {
	attempts := 0
	runner := &Runner{
		EngineFactory: func(_ string) (EngineRunner, error) {
			return &mockRetryEngine{attempts: &attempts, failUntil: 100}, nil
		},
	}
	// Use an engine that returns a non-429 error
	runner.EngineFactory = func(_ string) (EngineRunner, error) {
		return &mockEngine{err: fmt.Errorf("some other error")}, nil
	}
	task := Task{ID: "no-retry-test", Prompt: "test", VerifyScript: "exit 0"}
	result := runner.RunTask(context.Background(), task)
	if result.Passed {
		t.Error("expected failure")
	}
}

func TestRunTask(t *testing.T) {
	runner := &Runner{
		EngineFactory: func(_ string) (EngineRunner, error) {
			return &mockEngine{
				records: []ToolCallRecord{
					{Name: "bash", DurationMs: 100},
					{Name: "write", DurationMs: 50},
				},
			}, nil
		},
	}

	task := Task{
		ID:           "test-task",
		Category:     "test",
		Prompt:       "do something",
		VerifyScript: "exit 0",
	}

	result := runner.RunTask(context.Background(), task)

	if !result.Passed {
		t.Errorf("expected passed, got error: %s", result.Error)
	}
	if result.TaskID != "test-task" {
		t.Errorf("task_id = %q, want %q", result.TaskID, "test-task")
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("tool_calls = %d, want 2", len(result.ToolCalls))
	}
	if result.ToolCalls[0] != "bash" {
		t.Errorf("tool_calls[0] = %q, want %q", result.ToolCalls[0], "bash")
	}
	if result.DurationMs < 0 {
		t.Error("duration_ms should be >= 0")
	}
}

func TestRunTaskSetup(t *testing.T) {
	var capturedDir string
	runner := &Runner{
		EngineFactory: func(workDir string) (EngineRunner, error) {
			capturedDir = workDir
			return &mockEngine{}, nil
		},
	}

	task := Task{
		ID:           "setup-test",
		Category:     "test",
		Prompt:       "check setup",
		SetupScript:  "echo hello > setup_result.txt",
		VerifyScript: "[ -f setup_result.txt ]",
	}

	result := runner.RunTask(context.Background(), task)

	if !result.Passed {
		t.Errorf("expected passed, error: %s", result.Error)
	}
	if capturedDir == "" {
		t.Error("engine factory was not called")
	}
}

func TestRunTaskVerifyFail(t *testing.T) {
	runner := &Runner{
		EngineFactory: func(_ string) (EngineRunner, error) {
			return &mockEngine{}, nil
		},
	}

	task := Task{
		ID:           "fail-test",
		Category:     "test",
		Prompt:       "do nothing",
		VerifyScript: "exit 1",
	}

	result := runner.RunTask(context.Background(), task)

	if result.Passed {
		t.Error("expected not passed")
	}
	if !strings.Contains(result.Error, "verification failed") {
		t.Errorf("error = %q, want 'verification failed'", result.Error)
	}
}

func TestRunSuite(t *testing.T) {
	runner := &Runner{
		EngineFactory: func(_ string) (EngineRunner, error) {
			return &mockEngine{}, nil
		},
	}

	suite := Suite{
		Name: "test-suite",
		Tasks: []Task{
			{ID: "t1", Prompt: "a", VerifyScript: "exit 0"},
			{ID: "t2", Prompt: "b", VerifyScript: "exit 1"},
		},
	}

	results := runner.RunSuite(context.Background(), suite)

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if !results[0].Passed {
		t.Error("first task should pass")
	}
	if results[1].Passed {
		t.Error("second task should fail")
	}
}

func TestResultJSON(t *testing.T) {
	result := Result{
		TaskID:    "test",
		Category:  "file-ops",
		Passed:    true,
		ToolCalls: []string{"bash", "write"},
		DurationMs: 500,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.TaskID != "test" {
		t.Errorf("task_id = %q, want %q", decoded.TaskID, "test")
	}
	if !decoded.Passed {
		t.Error("expected passed = true")
	}
	if len(decoded.ToolCalls) != 2 {
		t.Errorf("tool_calls len = %d, want 2", len(decoded.ToolCalls))
	}
}

func TestParseCLIArgsHelp(t *testing.T) {
	_, err := ParseCLIArgs([]string{"--help"})
	if err == nil {
		t.Fatal("expected error for --help")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage message, got: %v", err)
	}

	_, err = ParseCLIArgs([]string{"-h"})
	if err == nil {
		t.Fatal("expected error for -h")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage message, got: %v", err)
	}
}

func TestParseCLIArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		suite   string
		task    string
	}{
		{"suite", []string{"--suite=benchmarks/ops"}, false, "benchmarks/ops", ""},
		{"task", []string{"--task=benchmarks/ops/001"}, false, "", "benchmarks/ops/001"},
		{"both", []string{"--suite=a", "--task=b"}, true, "", ""},
		{"neither", []string{}, true, "", ""},
		{"unknown", []string{"--foo=bar"}, true, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseCLIArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil {
				if a.SuitePath != tt.suite {
					t.Errorf("suite = %q, want %q", a.SuitePath, tt.suite)
				}
				if a.TaskPath != tt.task {
					t.Errorf("task = %q, want %q", a.TaskPath, tt.task)
				}
			}
		})
	}
}

func TestRunCLI(t *testing.T) {
	// Create a task directory
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "001-test")
	os.Mkdir(taskDir, 0o755)
	os.WriteFile(filepath.Join(taskDir, "prompt.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("exit 0"), 0o644)

	factory := func(_ string) (EngineRunner, error) {
		return &mockEngine{}, nil
	}

	var buf strings.Builder
	err := RunCLI(context.Background(), []string{"--suite=" + dir}, factory, &buf)
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}

	var results []Result
	if err := json.Unmarshal([]byte(buf.String()), &results); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected passed, error: %s", results[0].Error)
	}
}
