package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", cfg.Provider)
	}
	if cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("expected default model, got %s", cfg.Model)
	}
	if cfg.MaxTokens != 8192 {
		t.Errorf("expected 8192 max tokens, got %d", cfg.MaxTokens)
	}
}

func TestLoadMissingFiles(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load should not error on missing files: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("expected defaults, got provider=%s", cfg.Provider)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	configJSON := `{"provider":"openai","model":"gpt-4o","maxTokens":4096}`
	if err := os.WriteFile(filepath.Join(dir, "nanocode.json"), []byte(configJSON), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("expected openai, got %s", cfg.Provider)
	}
	if cfg.Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", cfg.Model)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("expected 4096, got %d", cfg.MaxTokens)
	}
	if cfg.ProjectDir != dir {
		t.Errorf("expected ProjectDir=%s, got %s", dir, cfg.ProjectDir)
	}
}

func TestLoadGlobalAndProjectMerge(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalDir)

	globalCfgDir := filepath.Join(globalDir, "nanocode")
	os.MkdirAll(globalCfgDir, 0755)
	globalJSON := `{"provider":"anthropic","apiKey":"global-key","model":"claude-sonnet-4-20250514"}`
	os.WriteFile(filepath.Join(globalCfgDir, "config.json"), []byte(globalJSON), 0644)

	projectJSON := `{"model":"claude-opus-4-20250514"}`
	os.WriteFile(filepath.Join(projectDir, "nanocode.json"), []byte(projectJSON), 0644)

	cfg, err := Load(projectDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("expected anthropic from global, got %s", cfg.Provider)
	}
	if cfg.Model != "claude-opus-4-20250514" {
		t.Errorf("expected project model override, got %s", cfg.Model)
	}
	if cfg.APIKey != "global-key" {
		t.Errorf("expected global apiKey, got %s", cfg.APIKey)
	}
}

func TestEnvExpansion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_API_KEY", "sk-test-123")

	configJSON := `{"apiKey":"$TEST_API_KEY"}`
	os.WriteFile(filepath.Join(dir, "nanocode.json"), []byte(configJSON), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.APIKey != "sk-test-123" {
		t.Errorf("expected expanded env var, got %s", cfg.APIKey)
	}
}

func TestMergeTools(t *testing.T) {
	base := DefaultConfig()
	overlay := &Config{
		Tools: map[string]ToolConfig{
			"bash": {Allow: []string{"git *"}, Deny: []string{"rm -rf /"}},
		},
	}
	result := merge(base, overlay)
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool config, got %d", len(result.Tools))
	}
	if result.Tools["bash"].Allow[0] != "git *" {
		t.Errorf("expected bash allow rule")
	}
}

func TestProjectDirPreservedAfterMerge(t *testing.T) {
	dir := t.TempDir()
	configJSON := `{"provider":"openai"}`
	os.WriteFile(filepath.Join(dir, "nanocode.json"), []byte(configJSON), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectDir != dir {
		t.Errorf("ProjectDir should be preserved after merge, got %s", cfg.ProjectDir)
	}
}

func TestMergeDisableReflection(t *testing.T) {
	base := DefaultConfig()
	if base.DisableReflection {
		t.Fatal("default DisableReflection should be false")
	}

	overlay := &Config{DisableReflection: true}
	result := merge(base, overlay)
	if !result.DisableReflection {
		t.Error("expected DisableReflection=true after merge with overlay that sets it")
	}

	// Verify false overlay does not override true base
	base2 := &Config{DisableReflection: true}
	overlay2 := &Config{}
	result2 := merge(base2, overlay2)
	if !result2.DisableReflection {
		t.Error("expected DisableReflection=true to be preserved when overlay is false")
	}
}

func TestLoadAutoApprove(t *testing.T) {
	dir := t.TempDir()
	configJSON := `{
		"tools": {
			"bash": {
				"autoApprove": ["ls *", "pwd", "cat *"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "nanocode.json"), []byte(configJSON), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	bashCfg, ok := cfg.Tools["bash"]
	if !ok {
		t.Fatal("expected bash tool config")
	}
	if len(bashCfg.AutoApprove) != 3 {
		t.Errorf("expected 3 autoApprove patterns, got %d", len(bashCfg.AutoApprove))
	}
	if bashCfg.AutoApprove[0] != "ls *" {
		t.Errorf("expected first pattern 'ls *', got %q", bashCfg.AutoApprove[0])
	}
}
