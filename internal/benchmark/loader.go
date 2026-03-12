package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// taskConfig holds optional metadata from config.json in a task directory.
type taskConfig struct {
	Category      string   `json:"category"`
	ExpectedTools []string `json:"expected_tools"`
}

// LoadSuite reads a suite from a directory structure.
// Each subdirectory is a task containing:
//
//	prompt.txt   - the user prompt (required)
//	setup.sh     - optional setup script
//	verify.sh    - verification script (required)
//	config.json  - optional metadata (category, expected_tools)
func LoadSuite(dir string) (*Suite, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading suite directory %s: %w", dir, err)
	}

	suite := &Suite{
		Name: filepath.Base(dir),
	}

	// Collect subdirectories sorted by name
	var taskDirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			taskDirs = append(taskDirs, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(taskDirs)

	for _, taskDir := range taskDirs {
		task, err := LoadTask(taskDir)
		if err != nil {
			return nil, fmt.Errorf("loading task %s: %w", taskDir, err)
		}
		// Default category to suite name if not set
		if task.Category == "" {
			task.Category = suite.Name
		}
		suite.Tasks = append(suite.Tasks, *task)
	}

	return suite, nil
}

// LoadTask reads a single task from a directory.
func LoadTask(dir string) (*Task, error) {
	task := &Task{
		ID: filepath.Base(dir),
	}

	// Read prompt.txt (required)
	promptBytes, err := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if err != nil {
		return nil, fmt.Errorf("reading prompt.txt: %w", err)
	}
	task.Prompt = strings.TrimSpace(string(promptBytes))

	// Read verify.sh (required)
	verifyBytes, err := os.ReadFile(filepath.Join(dir, "verify.sh"))
	if err != nil {
		return nil, fmt.Errorf("reading verify.sh: %w", err)
	}
	task.VerifyScript = string(verifyBytes)

	// Read setup.sh (optional)
	setupBytes, err := os.ReadFile(filepath.Join(dir, "setup.sh"))
	if err == nil {
		task.SetupScript = string(setupBytes)
	}

	// Read config.json (optional)
	configBytes, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err == nil {
		var cfg taskConfig
		if jsonErr := json.Unmarshal(configBytes, &cfg); jsonErr != nil {
			return nil, fmt.Errorf("parsing config.json: %w", jsonErr)
		}
		task.Category = cfg.Category
		task.ExpectedTools = cfg.ExpectedTools
	}

	return task, nil
}
