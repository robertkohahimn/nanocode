package main

import "testing"

func TestParseArgsYesFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantConfirm bool
		wantPrompt  string
	}{
		{
			name:        "no flags",
			args:        []string{"hello"},
			wantConfirm: false,
			wantPrompt:  "hello",
		},
		{
			name:        "--yes flag",
			args:        []string{"--yes", "hello"},
			wantConfirm: true,
			wantPrompt:  "hello",
		},
		{
			name:        "-y flag",
			args:        []string{"-y", "hello"},
			wantConfirm: true,
			wantPrompt:  "hello",
		},
		{
			name:        "--yes at end",
			args:        []string{"hello", "--yes"},
			wantConfirm: true,
			wantPrompt:  "hello",
		},
		{
			name:        "--yes with other flags",
			args:        []string{"--yes", "--model", "gpt-4", "hello"},
			wantConfirm: true,
			wantPrompt:  "hello",
		},
		{
			name:        "-y with --session",
			args:        []string{"-y", "--session", "abc123", "hello"},
			wantConfirm: true,
			wantPrompt:  "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, _, _, _, _, autoConfirm, _ := parseArgs(tt.args)
			if autoConfirm != tt.wantConfirm {
				t.Errorf("autoConfirm = %v, want %v", autoConfirm, tt.wantConfirm)
			}
			if prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tt.wantPrompt)
			}
		})
	}
}

func TestParseArgsLogFlag(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantLog    string
		wantPrompt string
	}{
		{
			name:       "no --log",
			args:       []string{"hello"},
			wantLog:    "",
			wantPrompt: "hello",
		},
		{
			name:       "--log with file",
			args:       []string{"--log", "/tmp/nanocode.log", "hello"},
			wantLog:    "/tmp/nanocode.log",
			wantPrompt: "hello",
		},
		{
			name:       "--log stderr",
			args:       []string{"--log", "stderr", "hello"},
			wantLog:    "stderr",
			wantPrompt: "hello",
		},
		{
			name:       "--log with other flags",
			args:       []string{"--yes", "--log", "/tmp/out.log", "hello"},
			wantLog:    "/tmp/out.log",
			wantPrompt: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, _, _, _, _, _, logPath := parseArgs(tt.args)
			if logPath != tt.wantLog {
				t.Errorf("logPath = %q, want %q", logPath, tt.wantLog)
			}
			if prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tt.wantPrompt)
			}
		})
	}
}
