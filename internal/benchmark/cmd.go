package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// CLIArgs holds parsed benchmark subcommand arguments.
type CLIArgs struct {
	SuitePath string
	TaskPath  string
}

// ParseCLIArgs parses benchmark subcommand arguments.
// Expected forms:
//
//	benchmark --suite=path/to/suite
//	benchmark --task=path/to/task
func ParseCLIArgs(args []string) (CLIArgs, error) {
	var a CLIArgs
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return a, fmt.Errorf("usage: nanocode benchmark --suite=<path> | --task=<path>")
		}
		switch {
		case strings.HasPrefix(arg, "--suite="):
			a.SuitePath = strings.TrimPrefix(arg, "--suite=")
			if a.SuitePath == "" {
				return a, fmt.Errorf("missing value for --suite")
			}
		case strings.HasPrefix(arg, "--task="):
			a.TaskPath = strings.TrimPrefix(arg, "--task=")
			if a.TaskPath == "" {
				return a, fmt.Errorf("missing value for --task")
			}
		default:
			return a, fmt.Errorf("unknown benchmark argument: %s", arg)
		}
	}

	if a.SuitePath == "" && a.TaskPath == "" {
		return a, fmt.Errorf("benchmark requires --suite=<path> or --task=<path>")
	}
	if a.SuitePath != "" && a.TaskPath != "" {
		return a, fmt.Errorf("specify either --suite or --task, not both")
	}
	return a, nil
}

// RunCLI is the entry point for the benchmark subcommand.
// It loads tasks, runs them, and writes JSON results to w.
func RunCLI(ctx context.Context, args []string, factory func(string) (EngineRunner, error), w io.Writer) error {
	cliArgs, err := ParseCLIArgs(args)
	if err != nil {
		return err
	}

	runner := &Runner{EngineFactory: factory}

	var results []Result

	if cliArgs.TaskPath != "" {
		task, err := LoadTask(cliArgs.TaskPath)
		if err != nil {
			return fmt.Errorf("loading task: %w", err)
		}
		results = []Result{runner.RunTask(ctx, *task)}
	} else {
		suite, err := LoadSuite(cliArgs.SuitePath)
		if err != nil {
			return fmt.Errorf("loading suite: %w", err)
		}
		results = runner.RunSuite(ctx, *suite)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}
