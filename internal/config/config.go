package config

import (
	"encoding/json"
	"errors"
	"fmt"
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
	ProjectDir string                     `json:"-"` // set by Load(), not from JSON
	StrictMode bool                       `json:"-"` // CLI-only, disables auto-approval
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

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
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
