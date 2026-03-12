// Package benchmark provides infrastructure for running reproducible benchmark
// tasks against the nanocode engine to measure quality and correctness.
package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// DurationMs is milliseconds stored as int64 so the JSON tag "duration_ms"
// is truthful. time.Duration would marshal as nanoseconds.

// Task describes a single benchmark task.
type Task struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Prompt        string   `json:"prompt"`
	SetupScript   string   `json:"setup_script,omitempty"`
	VerifyScript  string   `json:"verify_script"`
	ExpectedTools []string `json:"expected_tools,omitempty"`
}

// Result captures the outcome of running a task.
type Result struct {
	TaskID       string   `json:"task_id"`
	Category     string   `json:"category"`
	Passed       bool     `json:"passed"`
	ToolCalls    []string `json:"tool_calls"`
	DurationMs   int64    `json:"duration_ms"`
	Error        string   `json:"error,omitempty"`
	VerifyOutput string   `json:"verify_output,omitempty"`
}

// Suite is a collection of tasks.
type Suite struct {
	Name  string `json:"name"`
	Tasks []Task `json:"tasks"`
}

// EngineRunner is the interface the benchmark runner needs from the engine.
// It decouples benchmark execution from the actual engine implementation.
type EngineRunner interface {
	// Run sends a prompt to the engine and returns tool call records.
	Run(ctx context.Context, prompt string) ([]ToolCallRecord, error)
}

// ToolCallRecord captures a tool call made during a run.
type ToolCallRecord struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
}

// Runner executes benchmark tasks.
type Runner struct {
	// EngineFactory creates an engine for each task with clean state.
	// It receives the working directory for the task.
	EngineFactory func(workDir string) (EngineRunner, error)
}

// RunTask executes a single benchmark task in an isolated temp directory.
func (r *Runner) RunTask(ctx context.Context, task Task) Result {
	result := Result{
		TaskID:   task.ID,
		Category: task.Category,
	}

	// Create isolated temp directory
	workDir, err := os.MkdirTemp("", "benchmark-"+task.ID+"-")
	if err != nil {
		result.Error = fmt.Sprintf("creating temp dir: %v", err)
		return result
	}
	defer os.RemoveAll(workDir)

	// Run setup script if provided
	if task.SetupScript != "" {
		if err := runScript(ctx, workDir, task.SetupScript); err != nil {
			result.Error = fmt.Sprintf("setup script failed: %v", err)
			return result
		}
	}

	// Create engine and run prompt
	eng, err := r.EngineFactory(workDir)
	if err != nil {
		result.Error = fmt.Sprintf("creating engine: %v", err)
		return result
	}

	start := time.Now()
	records, err := eng.Run(ctx, task.Prompt)
	result.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Error = fmt.Sprintf("engine run failed: %v", err)
		return result
	}

	// Collect tool call names
	for _, rec := range records {
		result.ToolCalls = append(result.ToolCalls, rec.Name)
	}

	// Run verify script
	if task.VerifyScript != "" {
		out, verifyErr := runScriptOutput(ctx, workDir, task.VerifyScript)
		result.VerifyOutput = out
		result.Passed = verifyErr == nil
		if verifyErr != nil && result.Error == "" {
			result.Error = fmt.Sprintf("verification failed: %v", verifyErr)
		}
	}

	return result
}

// RunSuite executes all tasks in a suite sequentially.
func (r *Runner) RunSuite(ctx context.Context, suite Suite) []Result {
	results := make([]Result, 0, len(suite.Tasks))
	for _, task := range suite.Tasks {
		if ctx.Err() != nil {
			break
		}
		results = append(results, r.RunTask(ctx, task))
	}
	return results
}

// runScript executes a shell script in the given directory.
func runScript(ctx context.Context, dir, script string) error {
	_, err := runScriptOutput(ctx, dir, script)
	return err
}

// runScriptOutput executes a shell script and returns its combined output.
func runScriptOutput(ctx context.Context, dir, script string) (string, error) {
	// Write script to a temp file in the work directory
	scriptPath := filepath.Join(dir, ".benchmark-script.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("writing script: %w", err)
	}
	defer os.Remove(scriptPath)

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
