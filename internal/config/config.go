package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is the top-level configuration structure.
type Config struct {
	Provider   string                     `json:"provider"`
	Model      string                     `json:"model"`
	APIKey     string                     `json:"apiKey"`
	MaxTokens  int                        `json:"maxTokens"`
	System     string                     `json:"system"`
	Tools      map[string]ToolConfig      `json:"tools"`
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
	BaseURL    string                     `json:"baseURL"`
	ProjectDir        string    `json:"-"`                  // set by Load(), not from JSON
	StrictMode        bool      `json:"-"`                  // CLI-only, disables auto-approval
	DisableReflection bool      `json:"disableReflection"`  // skip error reflection prompts
	LogWriter         io.Writer `json:"-"`                  // structured log output (nil = no logging)
	DisableSnapshot      bool      `json:"-"`                     // internal-only, not serialized
	DisableVerification  bool      `json:"disableVerification"`   // skip verification reminders
	CheckpointInterval   int       `json:"checkpointInterval"`    // 0 = disabled
	SummarizeThreshold  int `json:"summarizeThreshold"`  // 0 = disabled (use windowing)
	SummarizeKeepRecent int `json:"summarizeKeepRecent"` // messages to keep unsummarized

	// These track whether boolean fields were explicitly set in config JSON,
	// allowing merge to distinguish "not set" from "set to false".
	disableReflectionSet   bool
	disableVerificationSet bool
}

type ToolConfig struct {
	Allow       []string `json:"allow"`
	Deny        []string `json:"deny"`
	AutoApprove []string `json:"autoApprove"`
}

// MCPServerConfig describes an external MCP tool server.
type MCPServerConfig struct {
	Transport string   `json:"transport"` // "stdio" or "http"
	Command   string   `json:"command"`   // for stdio: command to run
	Args      []string `json:"args"`      // for stdio: command arguments
	Env       []string `json:"env"`       // for stdio: extra env vars
	URL       string   `json:"url"`       // for http: base URL
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Provider:  "anthropic",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 8192,
	}
}

// Load reads config from nanocode.json (project root) and
// ~/.config/nanocode/config.json (global), merging them.
func Load(projectDir string) (*Config, error) {
	cfg := DefaultConfig()
	cfg.ProjectDir = projectDir

	globalPath := filepath.Join(xdgConfigHome(), "nanocode", "config.json")
	global, err := loadFile(globalPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("loading global config: %w", err)
	}
	if global != nil {
		cfg = merge(cfg, global)
		cfg.ProjectDir = projectDir
	}

	if projectDir != "" {
		projectPath := filepath.Join(projectDir, "nanocode.json")
		project, err := loadFile(projectPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("loading project config: %w", err)
		}
		if project != nil {
			cfg = merge(cfg, project)
			cfg.ProjectDir = projectDir
		}
	}

	expandEnv(cfg)
	return cfg, nil
}

// configJSON mirrors Config but uses *bool for boolean fields so that
// merge can distinguish "not set" (nil) from "explicitly set to false".
type configJSON struct {
	Provider            string                     `json:"provider"`
	Model               string                     `json:"model"`
	APIKey              string                     `json:"apiKey"`
	MaxTokens           int                        `json:"maxTokens"`
	System              string                     `json:"system"`
	Tools               map[string]ToolConfig      `json:"tools"`
	MCPServers          map[string]MCPServerConfig `json:"mcpServers"`
	BaseURL             string                     `json:"baseURL"`
	DisableReflection   *bool                      `json:"disableReflection"`
	DisableVerification *bool                      `json:"disableVerification"`
	CheckpointInterval  int                        `json:"checkpointInterval"`
	SummarizeThreshold  int                        `json:"summarizeThreshold"`
	SummarizeKeepRecent int                        `json:"summarizeKeepRecent"`
}

func (cj *configJSON) toConfig() *Config {
	cfg := &Config{
		Provider:           cj.Provider,
		Model:              cj.Model,
		APIKey:             cj.APIKey,
		MaxTokens:          cj.MaxTokens,
		System:             cj.System,
		Tools:              cj.Tools,
		MCPServers:         cj.MCPServers,
		BaseURL:            cj.BaseURL,
		CheckpointInterval: cj.CheckpointInterval,
		SummarizeThreshold:  cj.SummarizeThreshold,
		SummarizeKeepRecent: cj.SummarizeKeepRecent,
	}
	if cj.DisableReflection != nil {
		cfg.DisableReflection = *cj.DisableReflection
		cfg.disableReflectionSet = true
	}
	if cj.DisableVerification != nil {
		cfg.DisableVerification = *cj.DisableVerification
		cfg.disableVerificationSet = true
	}
	return cfg
}

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cj configJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cj.toConfig(), nil
}

func merge(base, overlay *Config) *Config {
	if overlay.Provider != "" {
		base.Provider = overlay.Provider
	}
	if overlay.Model != "" {
		base.Model = overlay.Model
	}
	if overlay.APIKey != "" {
		base.APIKey = overlay.APIKey
	}
	if overlay.MaxTokens != 0 {
		base.MaxTokens = overlay.MaxTokens
	}
	if overlay.System != "" {
		base.System = overlay.System
	}
	if overlay.BaseURL != "" {
		base.BaseURL = overlay.BaseURL
	}
	if overlay.Tools != nil {
		if base.Tools == nil {
			base.Tools = make(map[string]ToolConfig)
		}
		for k, v := range overlay.Tools {
			base.Tools[k] = v
		}
	}
	if overlay.MCPServers != nil {
		if base.MCPServers == nil {
			base.MCPServers = make(map[string]MCPServerConfig)
		}
		for k, v := range overlay.MCPServers {
			base.MCPServers[k] = v
		}
	}
	if overlay.disableReflectionSet {
		base.DisableReflection = overlay.DisableReflection
	}
	if overlay.disableVerificationSet {
		base.DisableVerification = overlay.DisableVerification
	}
	if overlay.CheckpointInterval != 0 {
		base.CheckpointInterval = overlay.CheckpointInterval
	}
	if overlay.SummarizeThreshold != 0 {
		base.SummarizeThreshold = overlay.SummarizeThreshold
	}
	if overlay.SummarizeKeepRecent != 0 {
		base.SummarizeKeepRecent = overlay.SummarizeKeepRecent
	}
	return base
}

func expandEnv(cfg *Config) {
	cfg.APIKey = os.ExpandEnv(cfg.APIKey)
	cfg.BaseURL = os.ExpandEnv(cfg.BaseURL)
	cfg.System = os.ExpandEnv(cfg.System)
}

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".config")
	}
	return filepath.Join(home, ".config")
}
