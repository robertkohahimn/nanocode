package permission

import (
	"strings"
	"testing"
)

func TestChecker_AllowList(t *testing.T) {
	c := NewChecker([]string{"go", "git", "ls"}, nil)

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"go build ./...", false},
		{"git status", false},
		{"ls -la", false},
		{"rm -rf /", true},
		{"curl http://example.com", true},
	}

	for _, tt := range tests {
		err := c.Check(tt.cmd)
		if (err != nil) != tt.wantErr {
			t.Errorf("Check(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
		}
	}
}

func TestChecker_DenyList(t *testing.T) {
	c := NewChecker(nil, []string{"rm", "curl", "wget"})

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"go build ./...", false},
		{"ls -la", false},
		{"rm -rf /", true},
		{"curl http://example.com", true},
		{"wget http://example.com", true},
	}

	for _, tt := range tests {
		err := c.Check(tt.cmd)
		if (err != nil) != tt.wantErr {
			t.Errorf("Check(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
		}
	}
}

func TestChecker_AllowAndDeny(t *testing.T) {
	// Allow go and git, but deny git (deny takes precedence)
	c := NewChecker([]string{"go", "git"}, []string{"git"})

	if err := c.Check("go build"); err != nil {
		t.Errorf("go should be allowed: %v", err)
	}
	if err := c.Check("git push"); err == nil {
		t.Error("git should be denied (in deny list)")
	}
}

func TestChecker_Pipes(t *testing.T) {
	c := NewChecker([]string{"echo", "grep"}, nil)

	if err := c.Check("echo hello | grep hello"); err != nil {
		t.Errorf("pipe of allowed commands should pass: %v", err)
	}

	if err := c.Check("echo hello | rm -rf /"); err == nil {
		t.Error("pipe with disallowed command should fail")
	}
}

func TestChecker_CommandSubstitution(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil)

	if err := c.Check("echo $(rm -rf /)"); err == nil {
		t.Error("command substitution with disallowed command should fail")
	}
}

func TestChecker_Subshell(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil)

	if err := c.Check("(rm -rf /)"); err == nil {
		t.Error("subshell with disallowed command should fail")
	}
}

func TestChecker_MetaCommands(t *testing.T) {
	// Even with no allow/deny lists, meta-commands are always blocked
	c := NewChecker(nil, nil)

	tests := []struct {
		cmd     string
		wantErr bool
		errMsg  string
	}{
		{"eval echo hello", true, "eval"},
		{"exec /bin/sh", true, "exec"},
		{"bash -c 'rm -rf /'", true, "bash"},
		{"sh -c 'rm -rf /'", true, "sh"},
		// bash without -c is fine (if not in deny list)
		{"bash --version", false, ""},
	}

	for _, tt := range tests {
		err := c.Check(tt.cmd)
		if (err != nil) != tt.wantErr {
			t.Errorf("Check(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
		}
		if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
			t.Errorf("Check(%q) error %q should mention %q", tt.cmd, err, tt.errMsg)
		}
	}
}

func TestChecker_VariableExpansion(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil)

	if err := c.Check("$CMD arg1 arg2"); err == nil {
		t.Error("variable expansion in command name should be blocked")
	}
	if err := c.Check("${CMD} arg1"); err == nil {
		t.Error("braced variable expansion in command name should be blocked")
	}
}

func TestChecker_MalformedInput(t *testing.T) {
	c := NewChecker(nil, nil)

	if err := c.Check("if then else fi ;; {{"); err == nil {
		t.Error("malformed input should be rejected")
	}
}

func TestChecker_NoRestrictions(t *testing.T) {
	// No allow or deny = only meta-commands blocked
	c := NewChecker(nil, nil)

	if err := c.Check("rm -rf / && curl evil.com"); err != nil {
		t.Errorf("no restrictions should allow anything except meta-commands: %v", err)
	}
}

func TestChecker_ChainedCommands(t *testing.T) {
	c := NewChecker([]string{"go", "echo"}, nil)

	if err := c.Check("go build && echo done"); err != nil {
		t.Errorf("chained allowed commands should pass: %v", err)
	}
	if err := c.Check("go build && rm -rf /"); err == nil {
		t.Error("chain with disallowed command should fail")
	}
}

func TestChecker_Semicolons(t *testing.T) {
	c := NewChecker(nil, []string{"rm"})

	if err := c.Check("echo hello; rm -rf /"); err == nil {
		t.Error("semicolon-separated denied command should fail")
	}
}
