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
			prompt, _, _, _, autoConfirm := parseArgs(tt.args)
			if autoConfirm != tt.wantConfirm {
				t.Errorf("autoConfirm = %v, want %v", autoConfirm, tt.wantConfirm)
			}
			if prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tt.wantPrompt)
			}
		})
	}
}
