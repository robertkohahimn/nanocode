package permission

import (
	"strings"
	"testing"
)

func TestChecker_AllowList(t *testing.T) {
	c := NewChecker([]string{"go", "git", "ls"}, nil, nil)

	tests := []struct {
		cmd       string
		wantAllow bool
	}{
		{"go build ./...", true},
		{"git status", true},
		{"ls -la", true},
		{"rm -rf /", false},
		{"curl http://example.com", false},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed != tt.wantAllow {
			t.Errorf("Check(%q).Allowed = %v, want %v (reason: %s)", tt.cmd, result.Allowed, tt.wantAllow, result.Reason)
		}
	}
}

func TestChecker_DenyList(t *testing.T) {
	c := NewChecker(nil, []string{"rm", "curl", "wget"}, nil)

	tests := []struct {
		cmd       string
		wantAllow bool
	}{
		{"go build ./...", true},
		{"ls -la", true},
		{"rm -rf /", false},
		{"curl http://example.com", false},
		{"wget http://example.com", false},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed != tt.wantAllow {
			t.Errorf("Check(%q).Allowed = %v, want %v (reason: %s)", tt.cmd, result.Allowed, tt.wantAllow, result.Reason)
		}
	}
}

func TestChecker_AllowAndDeny(t *testing.T) {
	// Allow go and git, but deny git (deny takes precedence)
	c := NewChecker([]string{"go", "git"}, []string{"git"}, nil)

	if result := c.Check("go build"); !result.Allowed {
		t.Errorf("go should be allowed: %s", result.Reason)
	}
	if result := c.Check("git push"); result.Allowed {
		t.Error("git should be denied (in deny list)")
	}
}

func TestChecker_Pipes(t *testing.T) {
	c := NewChecker([]string{"echo", "grep"}, nil, nil)

	if result := c.Check("echo hello | grep hello"); !result.Allowed {
		t.Errorf("pipe of allowed commands should pass: %s", result.Reason)
	}

	if result := c.Check("echo hello | rm -rf /"); result.Allowed {
		t.Error("pipe with disallowed command should fail")
	}
}

func TestChecker_CommandSubstitution(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil, nil)

	if result := c.Check("echo $(rm -rf /)"); result.Allowed {
		t.Error("command substitution with disallowed command should fail")
	}
}

func TestChecker_Subshell(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil, nil)

	if result := c.Check("(rm -rf /)"); result.Allowed {
		t.Error("subshell with disallowed command should fail")
	}
}

func TestChecker_MetaCommands(t *testing.T) {
	// Even with no allow/deny lists, meta-commands are always blocked
	c := NewChecker(nil, nil, nil)

	tests := []struct {
		cmd        string
		wantDenied bool
		errMsg     string
	}{
		{"eval echo hello", true, "eval"},
		{"exec /bin/sh", true, "exec"},
		{"bash -c 'rm -rf /'", true, "bash"},
		{"sh -c 'rm -rf /'", true, "sh"},
		// bash without -c is fine (if not in deny list)
		{"bash --version", false, ""},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed == tt.wantDenied {
			t.Errorf("Check(%q).Allowed = %v, want %v", tt.cmd, result.Allowed, !tt.wantDenied)
		}
		if tt.wantDenied && !strings.Contains(result.Reason, tt.errMsg) {
			t.Errorf("Check(%q) reason %q should mention %q", tt.cmd, result.Reason, tt.errMsg)
		}
	}
}

func TestChecker_VariableExpansion(t *testing.T) {
	c := NewChecker([]string{"echo"}, nil, nil)

	if result := c.Check("$CMD arg1 arg2"); result.Allowed {
		t.Error("variable expansion in command name should be blocked")
	}
	if result := c.Check("${CMD} arg1"); result.Allowed {
		t.Error("braced variable expansion in command name should be blocked")
	}
}

func TestChecker_MalformedInput(t *testing.T) {
	c := NewChecker(nil, nil, nil)

	if result := c.Check("if then else fi ;; {{"); result.Allowed {
		t.Error("malformed input should be rejected")
	}
}

func TestChecker_NoRestrictions(t *testing.T) {
	// No allow or deny = only meta-commands blocked
	c := NewChecker(nil, nil, nil)

	if result := c.Check("rm -rf / && curl evil.com"); !result.Allowed {
		t.Errorf("no restrictions should allow anything except meta-commands: %s", result.Reason)
	}
}

func TestChecker_ChainedCommands(t *testing.T) {
	c := NewChecker([]string{"go", "echo"}, nil, nil)

	if result := c.Check("go build && echo done"); !result.Allowed {
		t.Errorf("chained allowed commands should pass: %s", result.Reason)
	}
	if result := c.Check("go build && rm -rf /"); result.Allowed {
		t.Error("chain with disallowed command should fail")
	}
}

func TestChecker_Semicolons(t *testing.T) {
	c := NewChecker(nil, []string{"rm"}, nil)

	if result := c.Check("echo hello; rm -rf /"); result.Allowed {
		t.Error("semicolon-separated denied command should fail")
	}
}

func TestChecker_AutoApprove(t *testing.T) {
	c := NewChecker(nil, nil, []string{"ls *", "pwd", "cat *"})

	tests := []struct {
		cmd         string
		wantAllowed bool
		wantAuto    bool
	}{
		{"ls -la", true, true},
		{"pwd", true, true},
		{"cat /etc/passwd", true, true},
		{"rm -rf /", true, false}, // allowed (no deny), but not auto-approved
		{"git status", true, false},
	}

	for _, tt := range tests {
		result := c.Check(tt.cmd)
		if result.Allowed != tt.wantAllowed {
			t.Errorf("Check(%q).Allowed = %v, want %v", tt.cmd, result.Allowed, tt.wantAllowed)
		}
		if result.AutoApprove != tt.wantAuto {
			t.Errorf("Check(%q).AutoApprove = %v, want %v", tt.cmd, result.AutoApprove, tt.wantAuto)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		// Exact match (no wildcard)
		{"pwd", "pwd", true},
		{"pwd", "pwdx", false},
		{"pwd", "xpwd", false},
		{"git status", "git status", true},
		{"git status", "git commit", false},

		// Wildcard at end
		{"ls *", "ls", true},
		{"ls *", "ls -la", true},
		{"ls *", "ls foo bar", true},
		{"ls *", "lsx", false},
		{"git *", "git status", true},
		{"git *", "git commit -m 'msg'", true},

		// Wildcard at start
		{"* --version", "go --version", true},
		{"* --version", "python --version", true},
		{"* --version", "go version", false},

		// Wildcard in middle
		{"git * --dry-run", "git push --dry-run", true},
		{"git * --dry-run", "git pull origin main --dry-run", true},
		{"git * --dry-run", "git push", false},

		// Multiple wildcards
		{"* status *", "git status -s", true},
		{"* * *", "a b c", true},
		{"* * *", "a b", false},

		// Empty pattern/text edge cases
		{"*", "", true},
		{"*", "anything", true},
		{"", "", true},
		{"", "x", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.text)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}
