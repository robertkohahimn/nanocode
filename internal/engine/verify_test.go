package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertkohahimn/nanocode/internal/provider"
	"github.com/robertkohahimn/nanocode/internal/store"
)

func TestIsVerifyCommandPositive(t *testing.T) {
	positives := []string{
		"go test ./...",
		"go build",
		"go vet ./...",
		"npm test",
		"npm run test",
		"npm run build",
		"yarn test",
		"yarn build",
		"pytest",
		"pytest -v tests/",
		"python -m pytest",
		"python -m unittest",
		"cargo test",
		"cargo build",
		"cargo check",
		"make test",
		"make build",
		"make check",
		"mvn test",
		"mvn compile",
		"gradle test",
		"gradle build",
		"dotnet test",
		"dotnet build",
		"mix test",
		"  go test ./...",  // leading whitespace
		"GO TEST ./...",    // uppercase
	}
	for _, cmd := range positives {
		if !IsVerifyCommand(cmd) {
			t.Errorf("expected IsVerifyCommand(%q) = true", cmd)
		}
	}
}

func TestIsVerifyCommandNegative(t *testing.T) {
	negatives := []string{
		"echo hello",
		"ls -la",
		"git status",
		"cat main.go",
		"cd /tmp",
		"rm -rf /",
		"grep test",
		"go run main.go",
		"make",
		"make testing-harness",
		"go testing",
		"elixir test.exs",
		"",
	}
	for _, cmd := range negatives {
		if IsVerifyCommand(cmd) {
			t.Errorf("expected IsVerifyCommand(%q) = false", cmd)
		}
	}
}

func TestVerifyStateTransitions(t *testing.T) {
	vs := &VerifyState{}

	// Initially not pending
	if vs.IsPending() {
		t.Error("expected IsPending() = false initially")
	}
	if len(vs.EditedFiles()) != 0 {
		t.Error("expected empty edited files initially")
	}

	// After marking an edit, should be pending
	vs.MarkEdit("foo.go")
	if !vs.IsPending() {
		t.Error("expected IsPending() = true after MarkEdit")
	}
	if len(vs.EditedFiles()) != 1 || vs.EditedFiles()[0] != "foo.go" {
		t.Errorf("expected [foo.go], got %v", vs.EditedFiles())
	}

	// After marking verified, should not be pending
	vs.MarkVerified()
	if vs.IsPending() {
		t.Error("expected IsPending() = false after MarkVerified")
	}
	if len(vs.EditedFiles()) != 0 {
		t.Error("expected empty edited files after MarkVerified")
	}
}

func TestVerifyStateDeduplication(t *testing.T) {
	vs := &VerifyState{}

	vs.MarkEdit("foo.go")
	vs.MarkEdit("bar.go")
	vs.MarkEdit("foo.go") // duplicate

	files := vs.EditedFiles()
	if len(files) != 2 {
		t.Errorf("expected 2 edited files, got %d: %v", len(files), files)
	}
}

func TestVerifyStateMultipleEditsBeforeVerify(t *testing.T) {
	vs := &VerifyState{}

	vs.MarkEdit("a.go")
	vs.MarkEdit("b.go")
	vs.MarkEdit("c.go")

	if !vs.IsPending() {
		t.Error("expected IsPending() = true")
	}
	if len(vs.EditedFiles()) != 3 {
		t.Errorf("expected 3 edited files, got %d", len(vs.EditedFiles()))
	}

	vs.MarkVerified()
	if vs.IsPending() {
		t.Error("expected IsPending() = false after MarkVerified")
	}
}

func TestEngineVerificationReminder(t *testing.T) {
	// Use a unique temp file to avoid read-before-write enforcement on existing files
	tmpFile := filepath.Join(t.TempDir(), "test_verify_harness.go")

	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// First response: edit a file via write tool
			{
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "write",
					Input: json.RawMessage(fmt.Sprintf(`{"file_path":%q,"content":"package main"}`, tmpFile)),
				}},
				{Type: provider.EventDone},
			},
			// Second response: try to finish without verification
			{
				{Type: provider.EventTextDelta, Text: "All done!"},
				{Type: provider.EventDone},
			},
			// Third response (after reminder): run tests then finish
			{
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc2",
					Name:  "bash",
					Input: json.RawMessage(`{"command":"go test ./..."}`),
				}},
				{Type: provider.EventDone},
			},
			// Fourth response: actually finish
			{
				{Type: provider.EventTextDelta, Text: "Verified and done!"},
				{Type: provider.EventDone},
			},
		},
	}

	eng := New(mp, st, testConfig(), nil, true)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "write a file", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have 4 provider calls: tool call, attempt to finish (gets reminder),
	// run tests, then finish
	if mp.callIdx != 4 {
		t.Errorf("expected 4 provider calls, got %d", mp.callIdx)
	}

	// The third request (index 2) should contain the verification reminder
	if len(mp.requests) < 3 {
		t.Fatalf("expected at least 3 requests, got %d", len(mp.requests))
	}
	thirdReq := mp.requests[2]
	lastMsg := thirdReq.Messages[len(thirdReq.Messages)-1]
	found := false
	for _, cb := range lastMsg.Content {
		if cb.Type == "text" && strings.Contains(cb.Text, "verification-reminder") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected verification reminder in third request messages")
		for i, msg := range thirdReq.Messages {
			t.Logf("  msg[%d] role=%s blocks=%d", i, msg.Role, len(msg.Content))
			for j, cb := range msg.Content {
				t.Logf("    block[%d] type=%s text=%s", j, cb.Type, truncateStr(cb.Text, 80))
			}
		}
	}
}

func TestEngineVerificationDisabled(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test_verify_disabled.go")

	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mp := &mockProvider{
		responses: [][]provider.Event{
			// First response: edit a file via write tool
			{
				{Type: provider.EventToolCallEnd, ToolCall: &provider.ToolCall{
					ID:    "tc1",
					Name:  "write",
					Input: json.RawMessage(fmt.Sprintf(`{"file_path":%q,"content":"package main"}`, tmpFile)),
				}},
				{Type: provider.EventDone},
			},
			// Second response: finish without verification (should succeed since disabled)
			{
				{Type: provider.EventTextDelta, Text: "All done!"},
				{Type: provider.EventDone},
			},
		},
	}

	cfg := testConfig()
	cfg.DisableVerification = true

	eng := New(mp, st, cfg, nil, true)
	ctx := context.Background()
	sessionID, _ := st.CreateSession(ctx, "/tmp")

	err = eng.Run(ctx, sessionID, "write a file", func(ev provider.Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should complete in 2 calls (no reminder injected)
	if mp.callIdx != 2 {
		t.Errorf("expected 2 provider calls, got %d", mp.callIdx)
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("...(%d more)", len(s)-n)
}
